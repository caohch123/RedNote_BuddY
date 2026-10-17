"""Single-account Spider_XHS bridge. JSON lines only; never an HTTP listener.

Authentication stays in memory and is exported only over the parent's private
stdio pipe. The Go application owns encrypted persistence and publish records.
"""
from __future__ import annotations

import base64
import dataclasses
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import threading
import time
import uuid
from datetime import datetime, timezone
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parent
VENDOR = ROOT / "vendor" / "Spider_XHS"
DATA_ROOT = ROOT.parent / "data"
sys.path.insert(0, str(VENDOR))


class BridgeError(Exception):
    def __init__(self, code: str, submission_unknown: bool = False):
        self.code = code
        self.submission_unknown = submission_unknown
        super().__init__(code)


MESSAGES = {
    "dependency_missing": "发布组件依赖缺失",
    "invalid_request": "请求参数无效",
    "unknown_method": "不支持的操作",
    "login_not_found": "二维码会话不存在",
    "login_failed": "二维码登录失败，请重试",
    "not_logged_in": "请先扫码登录",
    "session_invalid": "登录信息失效，请重新扫码",
    "account_check_failed": "暂时无法核实账号，请检查网络",
    "account_changed": "登录账号与确认发布的账号不一致，请重新确认",
    "validation_failed": "笔记或媒体不符合本工具支持范围",
    "asset_changed": "发布素材已变化，请重新确认发布稿",
    "topic_not_found": "未找到精确匹配的话题",
    "upload_failed": "素材上传失败，笔记尚未提交",
    "publish_rejected": "小红书拒绝了发布请求",
    "submission_unknown": "发布结果不明，请先在创作平台核实，禁止重复发布",
    "publisher_error": "发布组件操作失败",
}


def safe_account(value: dict) -> dict:
    # Explicit projection; neither login responses nor the Cookie map escape.
    user_id = str(value.get("userId") or value.get("user_id") or value.get("id") or "")
    nickname = str(value.get("userName") or value.get("nickname") or value.get("name") or "")[:100]
    avatar = str(value.get("userAvatar") or value.get("avatar") or value.get("image") or "")
    parsed = urlparse(avatar)
    if parsed.scheme != "https" or not (parsed.hostname or "").endswith((".xhscdn.com", ".xiaohongshu.com")):
        avatar = ""
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,100}", user_id):
        raise BridgeError("account_check_failed")
    return {"id": user_id, "nickname": nickname, "avatar": avatar,
            "red_id": str(value.get("redId") or value.get("red_id") or "")[:100]}


def utc(timestamp: float | None = None) -> str:
    return datetime.fromtimestamp(timestamp or time.time(), timezone.utc).isoformat().replace("+00:00", "Z")


def node_json(operation: str, payload: dict, timeout: float = 30) -> dict:
    flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
    proc = subprocess.run([os.environ.get("WORKBENCH_NODE_PATH") or "node", str(ROOT / "signer.cjs")],
                          input=json.dumps({"operation": operation, "payload": payload}, ensure_ascii=False),
                          text=True, encoding="utf-8", capture_output=True,
                          cwd=str(VENDOR), timeout=timeout, creationflags=flags)
    if proc.returncode:
        raise BridgeError("publisher_error")
    return json.loads(proc.stdout)


def memory_signer(api, data="", *, cookie, b1, dsl_pair, tier, sign_context, timeout=30):
    if tier not in {"0101", "0201"} or "a1=" not in cookie or b1 is None or ";" not in dsl_pair:
        raise BridgeError("publisher_error")
    payload = {"api": api, "data": data, "cookie": cookie, "b1": b1, "dslPair": dsl_pair, "tier": tier}
    payload.update(sign_context)
    if sign_context.get("dsProgram"):
        payload["dsfProgram"] = sign_context["dsProgram"]
    result = node_json("sign", payload, timeout)
    prefix = "mns0101_" if tier == "0101" else "mns0201_"
    lengths = {196, 197, 198} if tier == "0101" else {200}
    if not result.get("xs") or not result.get("xs_common") or not str(result.get("x3", "")).startswith(prefix) or len(result["x3"]) not in lengths:
        raise BridgeError("publisher_error")
    return result


def memory_profile(options, timeout=10):
    value = str(node_json("profile", dict(options), timeout).get("profileData") or "")
    if not re.fullmatch(r"[0-9a-f]{5000,20000}", value, re.I):
        raise BridgeError("publisher_error")
    return value


def media_upload_timeout(size: int) -> tuple[int, int]:
    transfer_seconds = max(60, min(900, 30 + (size + 256 * 1024 - 1) // (256 * 1024)))
    return 15, transfer_seconds


def load_upstream():
    # Dependencies and imports can log or raise credential-bearing exceptions.
    # The entry point disables normal stdout/stderr before reaching here.
    try:
        from loguru import logger
        logger.remove()
        from apis.xhs_creator_login_apis import XHSCreatorLoginApi
        from apis.xhs_creator_apis import XHS_Creator_Apis
        from xhs_utils.xhs_creator.auth import XHSCreatorAuth
        from xhs_utils.xhs_creator.http import CreatorHttpClient
        from xhs_utils.xhs_creator.state import CreatorDeviceProfile, CreatorSessionState
        from xhs_utils.xhs_creator import runtime, params
        import apis.xhs_creator_login_apis as login_module
        runtime.run_signer = memory_signer
        params.run_signer = memory_signer
        runtime.generate_profile_data = memory_profile
        login_module.generate_profile_data = memory_profile
        runtime.generate_b1 = lambda options=None, timeout=10: str(node_json("b1", dict(options or {}), timeout)["b1"])

        class PublisherHttpClient(CreatorHttpClient):
            def put(self, url, **kwargs):
                content = kwargs.get("data")
                if isinstance(content, (bytes, bytearray, memoryview)):
                    kwargs["timeout"] = media_upload_timeout(len(content))
                return super().put(url, **kwargs)

        return XHSCreatorLoginApi, XHS_Creator_Apis, XHSCreatorAuth, PublisherHttpClient, CreatorDeviceProfile, CreatorSessionState
    except (ImportError, ModuleNotFoundError):
        raise BridgeError("dependency_missing") from None


class PublisherBridge:
    def __init__(self, upstream=None):
        self.types = upstream
        self.lock = threading.RLock()
        self.network_lock = threading.RLock()
        self.auth = None
        self.api = None
        self.account = None
        self.login = None
        self.login_generation = 0

    def _types(self):
        if self.types is None:
            self.types = load_upstream()
        return self.types

    def _http(self):
        # The fixed upstream requirements support chrome146. Upstream's
        # chrome150 default is not available in curl_cffi==0.15.0.
        return self._types()[3](impersonate="chrome146")

    def health(self, _):
        self._types()
        if not (VENDOR / "node_modules" / "crypto-js" / "package.json").is_file():
            raise BridgeError("dependency_missing")
        flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
        try:
            probe = subprocess.run([os.environ.get("WORKBENCH_NODE_PATH") or "node", "--version"], capture_output=True, timeout=5, creationflags=flags)
        except (OSError, subprocess.TimeoutExpired):
            raise BridgeError("dependency_missing") from None
        if probe.returncode:
            raise BridgeError("dependency_missing")
        return {"available": True, "protocol": 1}

    def login_start(self, _):
        self._types()
        with self.lock:
            self.login_generation += 1
            generation = self.login_generation
            self.login = {"login_id": uuid.uuid4().hex, "status": "starting", "qr_image": "",
                          "expires_at": "", "message": "正在准备二维码，请稍候"}
            result = dict(self.login)
        threading.Thread(target=self._login_worker, args=(generation,), daemon=True).start()
        return result

    def _update_login(self, generation, **changes):
        with self.lock:
            if generation != self.login_generation:
                return False
            self.login.update(changes)
            return True

    def _login_worker(self, generation):
        login = None
        transferred = False
        try:
            Login, API, Auth, _, Profile, _ = self._types()
            login = Login(profile=Profile(cookies="", source="workbench:qrcode"), http_client=self._http())
            # A flagged session should be surfaced to the user rather than
            # silently cycling through dozens of new identities.
            cookies, session = login._prepare_login_session()
            login._complete_security()
            if not session.get("active"):
                success, _, details = login.generate_qrcode(login.profile.cookie_map)
                if not success or not details or not details.get("qr_url"):
                    raise BridgeError("login_failed")
                import qrcode
                from qrcode.image.pure import PyPNGImage
                png = io.BytesIO()
                qrcode.make(details["qr_url"], image_factory=PyPNGImage).save(png)
                deadline = time.time() + 120
                if not self._update_login(generation, status="waiting_scan", qr_image="data:image/png;base64," + base64.b64encode(png.getvalue()).decode(), expires_at=utc(deadline), message="请使用本人小红书 App 扫码并确认登录"):
                    return
                while time.time() < deadline:
                    time.sleep(1)
                    with self.lock:
                        if generation != self.login_generation:
                            return
                    detail = login.query_qrcode_status(details["qr_id"])
                    state = detail.get("status")
                    if detail.get("success") and state == 1:
                        cookies = detail["cookies"]
                        break
                    if state == 3:
                        self._update_login(generation, status="waiting_confirm", message="请在手机上确认登录")
                    elif state == 2:
                        self._update_login(generation, status="waiting_scan", message="请使用本人小红书 App 扫码")
                    elif state == 4:
                        self._update_login(generation, status="expired", qr_image="", message="二维码已过期，请重新生成")
                        return
                    else:
                        raise BridgeError("login_failed")
                else:
                    self._update_login(generation, status="expired", qr_image="", message="二维码已过期，请重新生成")
                    return
            success, user, cookies = login.get_user_info(cookies)
            if not success:
                raise BridgeError("login_failed")
            account = safe_account(user)
            auth = Auth.from_cookie(cookies, profile=login.profile, http_client=login.http,
                                    host_cookie_state=login.host_cookie_state(), cookie_source_url=login.creator_url)
            api = API(auth)
            with self.lock:
                if generation != self.login_generation:
                    return
                old = self.auth
                self.auth, self.api, self.account = auth, api, account
                self.login.update(status="logged_in", qr_image="", message="已登录本人账号", account=account)
                transferred = True
            if old:
                old.close()
        except Exception:
            self._update_login(generation, status="error", qr_image="", message="二维码登录失败，请检查网络后重新生成")
        finally:
            if login and not transferred:
                try:
                    login.close()
                except Exception:
                    pass

    def login_status(self, params):
        with self.lock:
            if not self.login or self.login["login_id"] != params.get("login_id"):
                raise BridgeError("login_not_found")
            return dict(self.login)

    def _check_account(self):
        if not self.api:
            return {"status": "logged_out", "message": "请扫码登录小红书账号"}
        with self.network_lock:
            success, _, result = self.api.get_user_info()
        if not success:
            # A confirmed auth rejection is distinct from a transient network
            # failure, which must not erase encrypted login material.
            code = (result or {}).get("code")
            if code in {-100, -101, -102, -10000, 401}:
                return {"status": "expired", "message": "登录已失效，请重新扫码"}
            raise BridgeError("account_check_failed")
        self.account = safe_account((result or {}).get("data") or {})
        return {"status": "logged_in", "account": self.account, "message": "账号登录有效"}

    def account_status(self, _):
        return self._check_account()

    def account_export(self, _):
        with self.lock:
            auth, account = self.auth, self.account
            if not auth or not account:
                raise BridgeError("not_logged_in")
            account = dict(account)
        profile = auth.profile
        state = {"format": 1, "cookies": profile.cookie_map,
                 "host_cookie_state": auth._cookie_store.export_state(),
                 "dsl": profile.dsl, "ds_program": profile.ds_program,
                 "local_storage": dict(profile.local_storage), "session_storage": dict(profile.session_storage),
                 "session_state": dataclasses.asdict(profile.session), "account": account}
        session = base64.b64encode(json.dumps(state, ensure_ascii=False, separators=(",", ":")).encode()).decode()
        return {"session": session, "account": account}

    def account_restore(self, params):
        session = params.get("session")
        if not isinstance(session, str) or len(session) > 4 * 1024 * 1024:
            raise BridgeError("session_invalid")
        try:
            state = json.loads(base64.b64decode(session, validate=True))
            if state.get("format") != 1:
                raise ValueError()
            _, API, Auth, _, Profile, Session = self._types()
            profile = Profile(cookies=state["cookies"], dsl=state["dsl"], ds_program=state["ds_program"],
                              local_storage=state["local_storage"], session_storage=state["session_storage"],
                              session=Session(**state["session_state"]), source="workbench:restore")
            auth = Auth.from_cookie(state["cookies"], profile=profile, host_cookie_state=state["host_cookie_state"], http_client=self._http())
            old = self.auth
            self.auth, self.api = auth, API(auth)
            self.account = None
            if old:
                old.close()
        except Exception:
            raise BridgeError("session_invalid") from None
        return self._check_account()

    def logout(self, _):
        with self.lock:
            self.login_generation += 1
            self.login = None
            auth = self.auth
            self.auth = self.api = self.account = None
        if auth:
            auth.close()
        return {}

    def publish(self, params):
        publication = params.get("publication") or {}
        data_root = params.get("data_root")
        expected_account = params.get("expected_account_id")
        if not isinstance(expected_account, str) or not expected_account:
            raise BridgeError("validation_failed")
        validated = validate_publication(publication, data_root=data_root)
        # Restore the snapshot supplied by Go, never trust the last worker
        # account to match the confirmed account in a different session.
        restored = self.account_restore({"session": params.get("session")})
        if restored.get("status") != "logged_in":
            raise BridgeError("session_invalid")
        if restored["account"]["id"] != expected_account:
            raise BridgeError("account_changed")
        api = self.api
        with self.network_lock:
            data = prepare_payload(api, publication, validated)
            return submit_once(api, data, publication, self.account)

    def call(self, method, params):
        if not isinstance(params, dict):
            raise BridgeError("invalid_request")
        aliases = {"login_poll": "login_status", "account_clear": "logout"}
        method = aliases.get(method, method)
        if method not in {"health", "login_start", "login_status", "account_restore", "account_export", "account_status", "logout", "publish"}:
            raise BridgeError("unknown_method")
        return getattr(self, method)(params)


def validate_publication(publication, data_root=None):
    if data_root is not None and (not isinstance(data_root, str) or not Path(data_root).is_absolute()):
        raise BridgeError("validation_failed")
    asset_root = Path(data_root).resolve() if data_root else DATA_ROOT.resolve()
    title, body = publication.get("title"), publication.get("body")
    topics = publication.get("topics", [])
    if not isinstance(title, str) or not 1 <= len(title) <= 20 or not isinstance(body, str) or len(body) > 1000:
        raise BridgeError("validation_failed")
    if not isinstance(topics, list) or len(topics) > 10 or any(not isinstance(t, str) or not t or len(t) > 30 or re.search(r"[#\r\n\[\]]", t) for t in topics):
        raise BridgeError("validation_failed")
    # Include actual Creator topic markup in the tool's 1000-character limit.
    if len(body + "".join(f" #{topic}[话题]# " for topic in topics)) > 1000:
        raise BridgeError("validation_failed")
    if publication.get("visibility") not in {"public", "private"}:
        raise BridgeError("validation_failed")
    assets = publication.get("assets")
    media = publication.get("media_type")
    if not isinstance(assets, list) or not assets:
        raise BridgeError("validation_failed")
    if media == "image":
        if not 1 <= len(assets) <= 18 or any(a.get("kind") not in {"image", "cover"} for a in assets):
            raise BridgeError("validation_failed")
    elif media == "video":
        if len(assets) != 2 or sum(a.get("kind") == "video" for a in assets) != 1 or sum(a.get("kind") == "cover" for a in assets) != 1:
            raise BridgeError("validation_failed")
    else:
        raise BridgeError("validation_failed")
    paths = []
    for asset in assets:
        try:
            path = Path(asset["path"]).resolve(strict=True)
            path.relative_to(asset_root)
            if not path.is_file():
                raise ValueError()
            expected = asset.get("sha256")
            if not isinstance(expected, str) or not re.fullmatch(r"[0-9a-f]{64}", expected):
                raise ValueError()
            is_video = asset.get("kind") == "video"
            if path.suffix.lower() not in ({".mp4"} if is_video else {".png", ".jpg", ".jpeg"}):
                raise ValueError()
            size = path.stat().st_size
            if size <= 0 or size > (1024 ** 3 if is_video else 20 * 1024 ** 2):
                raise ValueError()
            with path.open("rb") as handle:
                digest = hashlib.file_digest(handle, "sha256").hexdigest()
        except (ValueError, KeyError, OSError):
            raise BridgeError("validation_failed") from None
        if digest != expected:
            raise BridgeError("asset_changed")
        paths.append((asset, path))
    return paths


def checked_upload(api, path, media, expected):
    # Upload the same in-memory bytes whose digest was verified, closing the
    # path-validation/upload race while the account/network check is running.
    try:
        content = path.read_bytes()
        if hashlib.sha256(content).hexdigest() != expected:
            raise BridgeError("asset_changed")
        success, _, result = api.upload_media(content, media)
    except BridgeError:
        raise
    except Exception:
        raise BridgeError("upload_failed") from None
    if not success or not result:
        raise BridgeError("upload_failed")
    if media == "image":
        result["mime_type"] = "image/jpeg" if content.startswith(b"\xff\xd8") else "image/png"
    return result


def prepare_payload(api, publication, paths):
    from xhs_utils.xhs_creator_util import get_post_note_image_data, get_post_note_video_data
    # Primary reference: ReaJason/xhs create_note uses int(is_private).
    privacy = {"public": 0, "private": 1}[publication["visibility"]]
    try:
        if publication["media_type"] == "image":
            infos = [checked_upload(api, path, "image", asset["sha256"]) for asset, path in paths]
            data = get_post_note_image_data(publication["title"], publication["body"], None, {}, privacy, infos)
        else:
            video_path = next(path for a, path in paths if a["kind"] == "video")
            cover_path = next(path for a, path in paths if a["kind"] == "cover")
            # Metadata only. The user's chosen cover is uploaded separately;
            # upstream's automatically extracted first frame is discarded.
            _, metadata = api.extract_video_cover_and_metadata(video_path.read_bytes())
            duration = (metadata.get("video") or {}).get("duration", 0)
            if not 0 < duration <= 900000:
                raise BridgeError("validation_failed")
            video_asset = next(a for a, _ in paths if a["kind"] == "video")
            cover_asset = next(a for a, _ in paths if a["kind"] == "cover")
            video = checked_upload(api, video_path, "video", video_asset["sha256"])
            cover = checked_upload(api, cover_path, "image", cover_asset["sha256"])
            data = get_post_note_video_data(publication["title"], publication["body"], None, {}, privacy, video, cover, metadata)
            data["video_info"]["cover"]["frame"].update(is_user_select=True, is_upload=True)
            # Creator-only auth can lack PC transcode permissions. Upload and
            # duration checks suffice; final server response is authoritative.
        data["common"].pop("post_loc", None)
        for topic in publication.get("topics") or []:
            success, _, response = api.get_topic(topic)
            choices = ((response or {}).get("data") or {}).get("topic_info_dtos") or []
            match = next((t for t in choices if str(t.get("name") or "") == topic), None)
            if not success or not match or not match.get("id"):
                raise BridgeError("topic_not_found")
            data["common"]["hash_tag"].append({"id": match["id"], "link": match.get("link", ""), "name": topic, "type": "topic"})
            data["common"]["desc"] += f" #{topic}[话题]# "
        return data
    except BridgeError:
        raise
    except Exception:
        raise BridgeError("upload_failed") from None


def submit_once(api, data, publication, account):
    from xhs_utils.xhs_creator_util import load_creator_rap_fingerprint_hex
    from xhs_utils.xhs_pc.params import generate_x_rap_param
    from xhs_utils.http_util import REQUEST_TIMEOUT
    post_api = "/web_api/sns/v2/note"
    try:
        headers, cookies, body = api._request_params(post_api, data, "POST", referer=api.base_url + "/", target_origin=api.edith_url, order_wire_headers=False)
        headers["x-rap-param"] = generate_x_rap_param(post_api, body, fingerprint_hex=load_creator_rap_fingerprint_hex())
    except Exception:
        # Nothing has reached the final creation endpoint.
        raise BridgeError("publish_rejected") from None
    try:
        # No retry decorator, loop, or upstream post_note invocation.
        response = api.http.post(api.edith_url + post_api, headers=headers, data=body.encode("utf-8"), cookies=cookies, proxies=api._proxies(), timeout=REQUEST_TIMEOUT)
        if response.status_code >= 500 or response.status_code == 408:
            raise BridgeError("submission_unknown", True)
        result = response.json()
    except BridgeError:
        raise
    except Exception:
        raise BridgeError("submission_unknown", True) from None
    if not isinstance(result, dict):
        raise BridgeError("submission_unknown", True)
    if result.get("success") is not True:
        if result.get("success") is False and (isinstance(result.get("code"), (str, int)) or isinstance(result.get("result"), (str, int))):
            raise BridgeError("publish_rejected")
        raise BridgeError("submission_unknown", True)
    payload = result.get("data") or {}
    if not isinstance(payload, dict):
        raise BridgeError("submission_unknown", True)
    note_id = str(payload.get("id") or payload.get("note_id") or payload.get("noteId") or "")
    if not re.fullmatch(r"[0-9a-fA-F]{24}", note_id):
        # success without an id cannot safely become a successful local record.
        raise BridgeError("submission_unknown", True)
    return {"note_id": note_id, "url": "https://www.xiaohongshu.com/explore/" + note_id,
            "published_at": utc(), "account": account, "visibility": publication["visibility"]}


def main():
    output = sys.stdout
    sink = open(os.devnull, "w", encoding="utf-8")
    sys.stdout = sink
    sys.stderr = sink
    # The bridge never reads ambient Cookie/env files or enables debug dumps.
    os.environ.pop("COOKIES", None)
    os.environ.pop("XHS_CREATOR_DEBUG", None)
    bridge = PublisherBridge()
    for line in sys.stdin:
        request_id = None
        request = None
        try:
            if len(line) > 8 * 1024 * 1024:
                raise BridgeError("invalid_request")
            request = json.loads(line)
            request_id = request["id"]
            if not isinstance(request_id, int) or not 0 <= request_id <= 2 ** 53:
                raise BridgeError("invalid_request")
            result = bridge.call(request["method"], request.get("params") or {})
            reply = {"id": request_id, "result": result}
        except BridgeError as exc:
            code = exc.code if exc.code in MESSAGES else "publisher_error"
            reply = {"id": request_id, "error": {"code": code, "message": MESSAGES[code], "submission_unknown": exc.submission_unknown}}
        except Exception:
            is_publish = isinstance(locals().get("request"), dict) and request.get("method") == "publish"
            code = "submission_unknown" if is_publish else "publisher_error"
            reply = {"id": request_id, "error": {"code": code, "message": MESSAGES[code], "submission_unknown": is_publish}}
        output.write(json.dumps(reply, ensure_ascii=False, separators=(",", ":")) + "\n")
        output.flush()
    bridge.logout({})


if __name__ == "__main__":
    main()
