import uuid
from typing import Any, Dict, List, Optional

import httpx

BASE_URL = "https://joycode-api.jd.com"
DEFAULT_MODEL = "JoyAI-Code"
CLIENT_VERSION = "2.4.5"
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"
)
TIMEOUT = 120.0

MODELS = [
    "JoyAI-Code",
    "MiniMax-M2.7",
    "Kimi-K2.6",
    "Kimi-K2.5",
    "GLM-5.1",
    "GLM-5",
    "GLM-4.7",
    "Doubao-Seed-2.0-pro",
]

CHAT_ENDPOINT = "/api/saas/openai/v1/chat/completions"


def _hex_id() -> str:
    return uuid.uuid4().hex


class Client:
    def __init__(self, pt_key: str, user_id: str):
        self.pt_key = pt_key
        self.user_id = user_id
        self.session_id = _hex_id()
        self._http = httpx.Client(timeout=TIMEOUT)

    def _headers(self) -> Dict[str, str]:
        return {
            "Content-Type": "application/json; charset=UTF-8",
            "ptKey": self.pt_key,
            "loginType": "N_PIN_PC",
            "User-Agent": USER_AGENT,
            "Accept": "*/*",
            "Accept-Encoding": "gzip, deflate, br",
            "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
            "Connection": "keep-alive",
        }

    def _prepare_body(self, extra: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        body: Dict[str, Any] = {
            "tenant": "JOYCODE",
            "userId": self.user_id,
            "client": "JoyCode",
            "clientVersion": CLIENT_VERSION,
            "sessionId": self.session_id,
        }
        if extra:
            if "chatId" not in extra:
                body["chatId"] = _hex_id()
            if "requestId" not in extra:
                body["requestId"] = _hex_id()
            body.update(extra)
        else:
            body["chatId"] = _hex_id()
            body["requestId"] = _hex_id()
        return body

    def post(self, endpoint: str, body: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        resp = self._http.post(
            BASE_URL + endpoint,
            json=self._prepare_body(body),
            headers=self._headers(),
        )
        if resp.status_code != 200:
            raise RuntimeError(f"API error {resp.status_code}: {resp.text}")
        return resp.json()

    def post_stream(self, endpoint: str, body: Optional[Dict[str, Any]] = None):
        req = self._http.build_request(
            "POST",
            BASE_URL + endpoint,
            json=self._prepare_body(body),
            headers=self._headers(),
        )
        resp = self._http.send(req, stream=True)
        if resp.status_code != 200:
            resp.read()
            resp.close()
            raise RuntimeError(f"API error {resp.status_code}: {resp.text}")
        return resp

    def list_models(self) -> List[Dict[str, Any]]:
        resp = self.post("/api/saas/models/v1/modelList")
        return resp.get("data", [])

    def web_search(self, query: str) -> List[Any]:
        body = {
            "messages": [{"role": "user", "content": query}],
            "stream": False,
            "model": "search_pro_jina",
            "language": "UNKNOWN",
        }
        resp = self.post("/api/saas/openai/v1/web-search", body)
        return resp.get("search_result", [])

    def rerank(self, query: str, documents: List[str], top_n: int) -> Dict[str, Any]:
        return self.post("/api/saas/openai/v1/rerank", {
            "model": "Qwen3-Reranker-8B",
            "query": query,
            "documents": documents,
            "top_n": top_n,
        })

    def user_info(self) -> Dict[str, Any]:
        return self.post("/api/saas/user/v1/userInfo")

    def validate(self) -> None:
        resp = self.user_info()
        code = resp.get("code", -1)
        if code != 0:
            msg = resp.get("msg", "unknown error")
            raise RuntimeError(
                f"credential validation failed (code={code}): {msg}"
            )
