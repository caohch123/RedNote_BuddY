"""Local, metadata-only wrapper around the frozen XHS-Downloader 2.8 API.

Upstream stays unmodified on disk. Its Volume constant is relocated at import;
network requests enforce TLS verification and checked redirects. The original
/xhs/detail handler is invoked; only allowlisted note data is returned.
"""
import argparse
import asyncio
from contextlib import asynccontextmanager
import importlib.abc
import importlib.machinery
import importlib.util
import ipaddress
import json
from pathlib import Path
import socket
import sys
from types import MethodType
from urllib.parse import urljoin, urlparse

ROOT = Path(__file__).resolve().parent
SAFE_FIELDS = frozenset(("作品ID", "作品链接", "作品标题", "作品描述", "作品类型", "作品标签", "作者ID", "作者昵称", "作者链接", "发布时间", "最后更新时间", "时间戳", "点赞数量", "收藏数量", "评论数量", "分享数量", "下载地址", "动图地址", "封面地址"))
ALLOWED_HOSTS = frozenset(("www.xiaohongshu.com", "xiaohongshu.com", "www.rednote.com", "rednote.com", "xhslink.com", "xhslink.cn"))


class DiscardLog:
    """Accept upstream console/widget output without retaining sensitive data."""

    def __call__(self, *args, **kwargs):
        pass

    def write(self, *args, **kwargs):
        # XHS 2.8's logging helper treats any target other than rich.print as
        # a widget and passes a Rich Text object plus scroll_end=True.
        pass

    def flush(self):
        pass


def checked_url(value: str) -> str:
    parsed = urlparse(value)
    if parsed.scheme not in ("http", "https") or parsed.hostname not in ALLOWED_HOSTS or parsed.username or parsed.password or parsed.port not in (None, 80, 443):
        raise ValueError("Unsupported XHS URL")
    # Normalize bare domains to the upstream extractor's www patterns and use
    # TLS's standard port even when an old shared link explicitly contains :80.
    hostname = parsed.hostname
    if hostname in ("xiaohongshu.com", "rednote.com"):
        hostname = "www." + hostname
    return parsed._replace(scheme="https", netloc=hostname, fragment="").geturl()


class RelocatedStaticLoader(importlib.machinery.SourceFileLoader):
    def __init__(self, fullname: str, path: str, volume: Path):
        super().__init__(fullname, path)
        self.volume = volume

    def get_code(self, fullname):
        text = self.get_source(fullname)
        original = 'VOLUME: Path = ROOT / "Volume"'
        if original not in text:
            raise RuntimeError("Pinned upstream Volume definition changed")
        return compile(text.replace(original, f"VOLUME: Path = Path({str(self.volume)!r})"), self.path, "exec")


class RelocatedStaticFinder(importlib.abc.MetaPathFinder):
    def __init__(self, volume: Path):
        self.volume = volume

    def find_spec(self, fullname, path=None, target=None):
        if fullname != "source.module.static":
            return None
        spec = importlib.machinery.PathFinder.find_spec(fullname, path)
        if spec is not None and spec.origin:
            spec.loader = RelocatedStaticLoader(fullname, spec.origin, self.volume)
        return spec


async def safe_request(self, value: str, content=True, cookie=None, proxy=None, **kwargs):
    """Keep upstream's browser impersonation; validate every redirect hop."""
    from curl_cffi.const import CurlOpt
    from curl_cffi.requests import AsyncSession

    if proxy:
        raise ValueError("Proxy overrides are not accepted")
    value = checked_url(value)
    for _ in range(5):
        parsed = urlparse(value)
        resolved = await asyncio.to_thread(socket.getaddrinfo, parsed.hostname, 443, 0, socket.SOCK_STREAM)
        addresses = list(dict.fromkeys(row[4][0] for row in resolved))
        if not addresses or any(not ipaddress.ip_address(address).is_global for address in addresses):
            raise ValueError("XHS host resolves to a restricted network")
        # Pin cURL to the validated address to prevent a second DNS lookup from
        # rebinding. The URL host still supplies TLS SNI and certificate checks.
        address = addresses[0]
        if ":" in address:
            address = f"[{address}]"
        async with AsyncSession(curl_options={CurlOpt.RESOLVE: [f"{parsed.hostname}:443:{address}".encode()]}, trust_env=False) as session:
            cookies = self.manager.cookie_str_to_dict(cookie) if cookie and parsed.hostname.endswith("xiaohongshu.com") else None
            response = await session.get(value, headers=self.manager.get_headers(value), cookies=cookies, timeout=25, verify=True, allow_redirects=False, impersonate=self.impersonate)
        if response.status_code in (301, 302, 303, 307, 308):
            location = response.headers.get("location")
            if not location:
                raise ValueError("Redirect missing location")
            value = checked_url(urljoin(value, location))
            continue
        response.raise_for_status()
        return response.text if content else value
    raise ValueError("Too many redirects")


def create_app(data_dir: Path):
    lock = json.loads((ROOT / "upstream.lock.json").read_text(encoding="utf-8"))
    source_path = ROOT / lock["source_directory"]
    if not (source_path / "source").exists():
        raise RuntimeError("Run engine/prepare.py before starting the collector")
    data_dir.mkdir(parents=True, exist_ok=True)
    sys.dont_write_bytecode = True
    sys.path.insert(0, str(source_path))
    sys.meta_path.insert(0, RelocatedStaticFinder(data_dir / "Volume"))
    from source.application.app import XHS
    from source.module import ExtractParams
    from fastapi import FastAPI, Request
    from fastapi.responses import JSONResponse

    xhs = XHS(work_path=str(data_dir), folder_name="media", record_data=False, download_record=False, note_format="", max_retry=0, image_format="jpeg", script_server=False)
    xhs.print.func = DiscardLog()
    xhs.html.request_url = MethodType(safe_request, xhs.html)
    upstream_app = FastAPI()
    xhs.setup_routes(upstream_app)
    handler = next(route.endpoint for route in upstream_app.routes if getattr(route, "path", None) == "/xhs/detail")

    @asynccontextmanager
    async def lifespan(app):
        async with xhs:
            yield

    app = FastAPI(title="XHS Workbench Collector", docs_url=None, redoc_url=None, lifespan=lifespan)

    @app.middleware("http")
    async def local_only(request: Request, call_next):
        if request.client is None or request.client.host not in ("127.0.0.1", "::1", "testclient"):
            return JSONResponse({"error": "Local access only"}, status_code=403)
        if request.headers.get("origin"):
            return JSONResponse({"error": "Browser access is not supported"}, status_code=403)
        try:
            return await call_next(request)
        except Exception:
            # Never let upstream exception representations log a Cookie or URL.
            return JSONResponse({"error": "Collector request failed"}, status_code=502)

    @app.get("/health")
    async def health():
        return {"ok": True, "name": lock["name"], "version": lock["version"], "commit": lock["commit"]}

    @app.post("/xhs/detail")
    async def detail(request: Request):
        if int(request.headers.get("content-length", "0")) > 128 * 1024:
            return JSONResponse({"error": "Request too large"}, status_code=413)
        try:
            payload = await request.json()
            value = checked_url(str(payload["url"]))
            params = ExtractParams(url=value, download=False, check_record=False, cookie=str(payload.get("cookie", "")), proxy=None)
            result = await handler(params)
            data = result.data
            if not isinstance(data, dict) or not data.get("作品ID"):
                return JSONResponse({"error": "笔记不可访问，请检查链接或 Cookie", "code": "XHS_NOTE_UNAVAILABLE"}, status_code=422)
            return {"data": {key: value for key, value in data.items() if key in SAFE_FIELDS}}
        except (KeyError, TypeError, ValueError):
            return JSONResponse({"error": "链接或请求格式无效", "code": "XHS_INVALID_INPUT"}, status_code=400)
        except Exception:
            return JSONResponse({"error": "采集连接失败或内容受限", "code": "XHS_FETCH_FAILED"}, status_code=502)

    return app


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--data-dir", type=Path, default=ROOT.parent / "data" / "engine")
    parser.add_argument("--port", type=int, default=5556)
    args = parser.parse_args()
    if sys.version_info < (3, 12):
        parser.error("Python 3.12 or newer is required")
    import uvicorn
    uvicorn.run(create_app(args.data_dir.resolve()), host="127.0.0.1", port=args.port, log_level="warning", access_log=False)


if __name__ == "__main__":
    main()
