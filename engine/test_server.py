"""Local launcher, isolation and request-boundary tests; no live XHS scraping."""
import importlib.util
import contextlib
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("workbench_collector_server", ROOT / "server.py")
server = importlib.util.module_from_spec(spec)
spec.loader.exec_module(server)


class URLBoundaryTests(unittest.TestCase):
    def test_only_supported_urls_and_ports(self):
        self.assertEqual(server.checked_url("http://xhslink.com/a/123"), "https://xhslink.com/a/123")
        self.assertEqual(server.checked_url("https://xiaohongshu.com:443/explore/123?xsec_token=sample#fragment"),"https://www.xiaohongshu.com/explore/123?xsec_token=sample")
        for value in ("http://127.0.0.1/x", "http://www.xiaohongshu.com.evil.example/explore/x", "https://user:secret@www.xiaohongshu.com/explore/x", "https://www.xiaohongshu.com:8080/explore/x", "file:///C:/secrets"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                server.checked_url(value)


class LauncherTests(unittest.TestCase):
    def test_actual_health_and_volume_isolation(self):
        lock = json.loads((ROOT / "upstream.lock.json").read_text(encoding="utf-8"))
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        with tempfile.TemporaryDirectory(prefix="xhs-engine-test-") as folder:
            data = Path(folder) / "engine-data"
            log = Path(folder) / "launcher.log"
            with log.open("w", encoding="utf-8") as output:
                process = subprocess.Popen([sys.executable, str(ROOT / "server.py"), "--data-dir", str(data), "--port", str(port)], cwd=ROOT, stdout=output, stderr=subprocess.STDOUT, creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
                try:
                    url = f"http://127.0.0.1:{port}"
                    for _ in range(100):
                        if process.poll() is not None:
                            self.fail("Collector exited: " + log.read_text(encoding="utf-8"))
                        try:
                            with urllib.request.urlopen(url + "/health", timeout=1) as response:
                                health = json.load(response)
                            break
                        except (urllib.error.URLError, ConnectionError, TimeoutError):
                            time.sleep(0.1)
                    else:
                        self.fail("Collector did not become ready")
                    self.assertTrue(health["ok"])
                    self.assertEqual(health["commit"], lock["commit"])
                    self.assertTrue((data / "Volume").is_dir())
                    self.assertFalse((ROOT / lock["source_directory"] / "Volume").exists())
                    request = urllib.request.Request(url + "/xhs/detail", data=json.dumps({"url": "http://127.0.0.1/secrets", "cookie": "secret-cookie"}).encode(), headers={"Content-Type": "application/json"})
                    with self.assertRaises(urllib.error.HTTPError) as result:
                        urllib.request.urlopen(request, timeout=3)
                    self.assertEqual(result.exception.code, 400)
                    self.assertNotIn("secret-cookie", result.exception.read().decode())
                    request = urllib.request.Request(url + "/health", headers={"Origin": "https://malicious.example"})
                    with self.assertRaises(urllib.error.HTTPError) as result:
                        urllib.request.urlopen(request, timeout=3)
                    self.assertEqual(result.exception.code, 403)
                finally:
                    if os.name == "nt" and process.poll() is None:
                        # Windows venv python.exe may be a redirector spawning the
                        # real interpreter; terminate the exact owned PID tree.
                        subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                    elif process.poll() is None:
                        process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
                    # Windows may release descendant SQLite handles after the
                    # parent wait returns; allow that completion before cleanup.
                    time.sleep(0.3)


class DetailHandlerTests(unittest.TestCase):
    def test_real_upstream_handler_reads_metadata_without_logging_secrets(self):
        from fastapi.testclient import TestClient

        note_id = "6ac5ed550000000014039b6f"
        note = {
            "noteId": note_id,
            "title": "采集回归测试",
            "desc": "完整正文与话题 #测试",
            "type": "normal",
            "time": 1720000000000,
            "lastUpdateTime": 1720000000000,
            "user": {"userId": "fixture-author", "nickname": "测试作者"},
            "tagList": [{"name": "测试"}],
            "interactInfo": {"likedCount": "23", "collectedCount": "4", "commentCount": "2", "shareCount": "1"},
            "imageList": [{"urlDefault": "https://sns-webpic.xhscdn.com/cache/sign/spectrum/fixture-image", "stream": {}}],
        }
        state = {"note": {"noteDetailMap": {note_id: {"note": note}}}}
        html = "<html><script>window.__INITIAL_STATE__=" + json.dumps(state, ensure_ascii=False) + "</script></html>"
        calls = []

        async def mocked_request(instance, value, content=True, cookie=None, **kwargs):
            calls.append((content, cookie))
            return html if content else value

        with tempfile.TemporaryDirectory(prefix="xhs-detail-test-") as folder:
            output = io.StringIO()
            with patch.object(server, "safe_request", mocked_request), contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                app = server.create_app(Path(folder))
                with TestClient(app) as client:
                    response = client.post("/xhs/detail", json={
                        "url": f"https://www.xiaohongshu.com/explore/{note_id}?xsec_token=fixture-token",
                        "download": False,
                        "check_record": False,
                        "cookie": "fixture-secret-cookie",
                    })
            self.assertEqual(response.status_code, 200, response.text)
            data = response.json()["data"]
            self.assertEqual(data["作品ID"], note_id)
            self.assertEqual(data["作品标题"], note["title"])
            self.assertEqual(data["作品描述"], note["desc"])
            self.assertEqual(data["点赞数量"], "23")
            self.assertEqual(len(data["下载地址"]), 1)
            self.assertNotIn("params", response.json())
            self.assertEqual(calls, [(True, "fixture-secret-cookie")])
            self.assertNotIn("fixture-secret-cookie", response.text)
            self.assertNotIn("fixture-secret-cookie", output.getvalue())
            self.assertNotIn("fixture-token", output.getvalue())


if __name__ == "__main__":
    unittest.main()
