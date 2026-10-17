import dataclasses
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("xhs_publisher_bridge", Path(__file__).parents[1] / "bridge.py")
bridge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bridge)


class BridgeTests(unittest.TestCase):
    def test_media_timeout_scales_for_real_video_and_is_bounded(self):
        self.assertEqual(bridge.media_upload_timeout(361396), (15, 60))
        self.assertEqual(bridge.media_upload_timeout(87482975), (15, 364))
        self.assertEqual(bridge.media_upload_timeout(1024 ** 3), (15, 900))

    def test_media_transport_extends_put_without_changing_get_or_post(self):
        HTTP = bridge.load_upstream()[3]
        client = HTTP(impersonate="chrome146")
        try:
            content = b"fixture"
            with patch.object(client, "request") as request:
                client.put("https://ros-upload.xiaohongshu.com/spectrum/fixture", data=content, timeout=15)
                self.assertEqual(request.call_args.args[0], "PUT")
                self.assertIs(request.call_args.kwargs["data"], content)
                self.assertEqual(request.call_args.kwargs["timeout"], (15, 60))
                self.assertEqual(request.call_count, 1)
                client.get("https://creator.xiaohongshu.com/fixture", timeout=15)
                self.assertEqual(request.call_args.kwargs["timeout"], 15)
                client.post("https://creator.xiaohongshu.com/fixture", data=b"fixture", timeout=15)
                self.assertEqual(request.call_args.kwargs["timeout"], 15)
        finally:
            client.close()

    def test_configured_node_is_used_for_signer_and_health(self):
        selected = r"C:\custom tools\chosen-node.exe"
        with patch.dict(bridge.os.environ, {"WORKBENCH_NODE_PATH": selected}), patch.object(bridge.subprocess, "run") as run:
            run.return_value = Mock(returncode=0, stdout='{}')
            bridge.node_json("profile", {})
            self.assertEqual(run.call_args.args[0][0], selected)
            worker = bridge.PublisherBridge(upstream=Mock())
            with patch.object(Path, "is_file", return_value=True):
                self.assertTrue(worker.health({})["available"])
            self.assertEqual(run.call_args.args[0], [selected, "--version"])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.data_patch = patch.object(bridge, "DATA_ROOT", self.root)
        self.data_patch.start()
        self.note = {"id": "publication", "title": "示例标题", "body": "示例正文", "topics": [], "visibility": "private", "media_type": "image", "assets": []}
        self.note["assets"] = [self.asset("first.png", "image"), self.asset("second.png", "image")]

    def tearDown(self):
        self.data_patch.stop()
        self.temp.cleanup()

    def asset(self, name, kind):
        path = self.root / name
        path.write_bytes(("fixture-" + name).encode())
        return {"id": name, "path": str(path), "kind": kind, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}

    def test_account_projection_and_identity_required(self):
        result = bridge.safe_account({"userId": "abc123", "userName": "本人", "redId": "red1", "avatar": "https://sns-avatar.xhscdn.com/a.png", "cookies": "SECRET"})
        self.assertEqual(set(result), {"id", "nickname", "red_id", "avatar"})
        self.assertNotIn("SECRET", json.dumps(result))
        with self.assertRaises(bridge.BridgeError):
            bridge.safe_account({"userName": "没有ID"})
        self.assertEqual(bridge.safe_account({"id": "abc", "avatar": "https://xhscdn.com.evil.example/a"})["avatar"], "")

    def test_hash_path_scope_and_unknown_visibility(self):
        paths = bridge.validate_publication(self.note)
        self.assertEqual([p.name for _, p in paths], ["first.png", "second.png"])
        (self.root / "first.png").write_bytes(b"changed")
        with self.assertRaises(bridge.BridgeError) as exc:
            bridge.validate_publication(self.note)
        self.assertEqual(exc.exception.code, "asset_changed")
        self.note["assets"] = [self.asset("fixed.png", "image")]
        self.note["visibility"] = "unknown"
        with self.assertRaises(bridge.BridgeError):
            bridge.validate_publication(self.note)
        self.note["visibility"] = "public"
        self.note["assets"][0]["path"] = str(self.root.parent / "outside.png")
        with self.assertRaises(bridge.BridgeError):
            bridge.validate_publication(self.note)

    def test_topic_markup_counts_against_limit(self):
        self.note["body"] = "字" * 997
        self.note["topics"] = ["学习"]
        with self.assertRaises(bridge.BridgeError):
            bridge.validate_publication(self.note)

    def test_custom_data_root_and_account_match_before_upload(self):
        with patch.object(bridge, "DATA_ROOT", self.root / "different-default"):
            with self.assertRaises(bridge.BridgeError):
                bridge.validate_publication(self.note)
            self.assertEqual(len(bridge.validate_publication(self.note, data_root=str(self.root))), 2)
        worker = bridge.PublisherBridge()
        worker.account_restore = Mock(return_value={"status": "logged_in", "account": {"id": "different-account"}})
        worker.api = Mock()
        with self.assertRaises(bridge.BridgeError) as exc:
            worker.publish({"publication": self.note, "expected_account_id": "confirmed-account", "session": "synthetic"})
        self.assertEqual(exc.exception.code, "account_changed")
        self.assertFalse(worker.api.upload_media.called)
        self.assertFalse(worker.api.http.post.called)

    def test_asset_changed_after_initial_validation_never_uploads(self):
        paths = bridge.validate_publication(self.note)
        paths[0][1].write_bytes(b"modified-after-confirmation")
        api = Mock()
        with self.assertRaises(bridge.BridgeError) as exc:
            bridge.prepare_payload(api, self.note, paths)
        self.assertEqual(exc.exception.code, "asset_changed")
        self.assertFalse(api.upload_media.called)

    def test_payload_preserves_order_and_visibility(self):
        api = Mock()
        api.upload_media.side_effect = lambda content, kind: (True, "", {"fileIds": Path(content.decode().removeprefix("fixture-")).stem, "width": 1200, "height": 1600})
        for visibility, value in [("public", 0), ("private", 1)]:
            self.note["visibility"] = visibility
            payload = bridge.prepare_payload(api, self.note, bridge.validate_publication(self.note))
            self.assertEqual(payload["common"]["privacy_info"]["type"], value)
            self.assertEqual([x["file_id"] for x in payload["image_info"]["images"]], ["spectrum/first", "spectrum/second"])
            self.assertNotIn("post_loc", payload["common"])

    def test_topic_requires_exact_name(self):
        api = Mock()
        api.upload_media.return_value = (True, "", {"fileIds": "f", "width": 10, "height": 10})
        api.get_topic.return_value = (True, "", {"data": {"topic_info_dtos": [{"id": "wrong", "name": "学习技巧"}]}})
        self.note["topics"] = ["学习"]
        with self.assertRaises(bridge.BridgeError) as exc:
            bridge.prepare_payload(api, self.note, bridge.validate_publication(self.note))
        self.assertEqual(exc.exception.code, "topic_not_found")
        api.get_topic.return_value = (True, "", {"data": {"topic_info_dtos": [{"id": "wrong", "name": "学习技巧"}, {"id": "exact", "name": "学习", "link": "link"}]}})
        payload = bridge.prepare_payload(api, self.note, bridge.validate_publication(self.note))
        self.assertEqual(payload["common"]["hash_tag"][0]["id"], "exact")
        self.assertEqual(payload["common"]["desc"], "示例正文 #学习[话题]# ")

    def test_video_uploads_confirmed_cover(self):
        self.note["media_type"] = "video"
        self.note["assets"] = [self.asset("video.mp4", "video"), self.asset("chosen-cover.png", "cover")]
        api = Mock()
        api.extract_video_cover_and_metadata.return_value = (b"unselected first frame", {"video": {"duration": 30000}, "audio": {"duration": 30000}})
        api.upload_media.side_effect = lambda content, kind: (True, "", {"fileIds": Path(content.decode().removeprefix("fixture-")).stem, "width": 720, "height": 1280, "video_id": "v"})
        payload = bridge.prepare_payload(api, self.note, bridge.validate_publication(self.note))
        self.assertEqual([call.args[0] for call in api.upload_media.call_args_list], [b"fixture-video.mp4", b"fixture-chosen-cover.png"])
        self.assertEqual(payload["video_info"]["cover"]["file_id"], "spectrum/chosen-cover")
        self.assertFalse(api.post_note.called)
        self.assertTrue(payload["video_info"]["cover"]["frame"]["is_upload"])

    def submission_api(self):
        api = Mock()
        api.base_url = "https://creator.xiaohongshu.com"
        api.edith_url = "https://edith.xiaohongshu.com"
        api._request_params.return_value = ({}, {}, "{}")
        api.http.post.return_value.status_code = 200
        return api

    def test_submission_once_and_unknown_no_retry(self):
        api = self.submission_api()
        api.http.post.side_effect = TimeoutError("Cookie SECRET")
        with patch("xhs_utils.xhs_pc.params.generate_x_rap_param", return_value="rap"), patch("xhs_utils.xhs_creator_util.load_creator_rap_fingerprint_hex", return_value="fingerprint"):
            with self.assertRaises(bridge.BridgeError) as exc:
                bridge.submit_once(api, {}, self.note, {"id": "abc"})
        self.assertTrue(exc.exception.submission_unknown)
        self.assertNotIn("SECRET", str(exc.exception))
        self.assertEqual(api.http.post.call_count, 1)
        self.assertFalse(api.post_note.called)

    def test_explicit_rejection_and_valid_success(self):
        api = self.submission_api()
        with patch("xhs_utils.xhs_pc.params.generate_x_rap_param", return_value="rap"), patch("xhs_utils.xhs_creator_util.load_creator_rap_fingerprint_hex", return_value="fingerprint"):
            api.http.post.return_value.json.return_value = {"success": False, "code": -1, "msg": "SECRET"}
            with self.assertRaises(bridge.BridgeError) as exc:
                bridge.submit_once(api, {}, self.note, {"id": "abc"})
            self.assertFalse(exc.exception.submission_unknown)
            api.http.post.return_value.json.return_value = {"success": True, "data": {}}
            with self.assertRaises(bridge.BridgeError) as exc:
                bridge.submit_once(api, {}, self.note, {"id": "abc"})
            self.assertTrue(exc.exception.submission_unknown)
            api.http.post.return_value.json.return_value = {"success": True, "data": {"id": "6ac5ed550000000014039b6f"}, "headers": "SECRET"}
            result = bridge.submit_once(api, {}, self.note, {"id": "abc"})
            self.assertEqual(result["note_id"], "6ac5ed550000000014039b6f")
            self.assertNotIn("SECRET", json.dumps(result))
        self.assertEqual(api.http.post.call_count, 3)  # three explicit test calls

    def test_server_error_always_unknown_even_with_error_json(self):
        api = self.submission_api()
        for status in [500, 502, 504, 408]:
            api.http.post.return_value.status_code = status
            api.http.post.return_value.json.return_value = {"success": False, "code": "backend-error"}
            with patch("xhs_utils.xhs_pc.params.generate_x_rap_param", return_value="rap"), patch("xhs_utils.xhs_creator_util.load_creator_rap_fingerprint_hex", return_value="fingerprint"):
                with self.assertRaises(bridge.BridgeError) as exc:
                    bridge.submit_once(api, {}, self.note, {"id": "abc"})
            self.assertTrue(exc.exception.submission_unknown)
        self.assertEqual(api.http.post.call_count, 4)  # four explicit test calls

    def test_qr_expiration_never_installs_account(self):
        login = Mock()
        login.profile.cookie_map = {"a1": "synthetic"}
        login._prepare_login_session.return_value = ({}, {"active": False})
        login.generate_qrcode.return_value = (True, "", {"qr_id": "mock", "qr_url": "https://example.invalid/login"})
        login.query_qrcode_status.return_value = {"status": 4, "success": False}
        worker = bridge.PublisherBridge(upstream=(Mock(return_value=login), Mock(), Mock(), Mock(), Mock(), Mock()))
        worker.login_generation = 1
        worker.login = {"login_id": "mock", "status": "starting"}
        with patch.object(bridge.time, "sleep", return_value=None):
            worker._login_worker(1)
        self.assertEqual(worker.login["status"], "expired")
        self.assertEqual(worker.login["qr_image"], "")
        self.assertIsNone(worker.auth)
        self.assertFalse(login.get_user_info.called)

    def test_upload_failure_not_submitted(self):
        api = Mock()
        api.upload_media.return_value = (False, "Cookie SECRET", None)
        with self.assertRaises(bridge.BridgeError) as exc:
            bridge.prepare_payload(api, self.note, bridge.validate_publication(self.note))
        self.assertEqual(exc.exception.code, "upload_failed")
        self.assertFalse(exc.exception.submission_unknown)
        self.assertFalse(api.http.post.called)

    def test_logout_cancels_old_login_before_account_install(self):
        started, release = threading.Event(), threading.Event()
        login = Mock()
        login.profile.cookie_map = {"a1": "synthetic"}
        login._prepare_login_session.return_value = ({}, {"active": True})
        def user_info(_):
            started.set();release.wait(2)
            return True, {"id": "abc", "nickname": "本人"}, {"a1": "synthetic"}
        login.get_user_info.side_effect = user_info
        fake_auth = Mock()
        types = (Mock(return_value=login), Mock(), fake_auth, Mock(), Mock(), Mock())
        worker = bridge.PublisherBridge(upstream=types)
        first = worker.login_start({})
        self.assertTrue(started.wait(2))
        worker.logout({})
        time_limit = threading.Event()
        # Waiting on mocked close observes that the stale thread reached its
        # finally without installing an account.
        login.close.side_effect = lambda: time_limit.set()
        release.set()
        self.assertTrue(time_limit.wait(2))
        self.assertIsNone(worker.auth)
        self.assertIsNone(worker.account)
        with self.assertRaises(bridge.BridgeError):
            worker.login_status({"login_id": first["login_id"]})


if __name__ == "__main__":
    unittest.main()
