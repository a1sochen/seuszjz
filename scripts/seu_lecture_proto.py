#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
东南大学研究生「素质讲座」活动预约信息抓取 —— 纯协议版

不依赖浏览器 / Selenium, 只用 requests + 标准库, 完整重放登录流程:

    ┌─ 统一身份认证 (auth.seu.edu.cn / CAS)
    │    GET  /dist/                                 建立 CAS 会话
    │    POST /auth/casback/verifyTgt                探测 TGT
    │    GET  /auth/casback/needCaptcha              是否需要验证码/二次校验
    │    POST /auth/casback/getChiperKey             取 RSA 公钥
    │    POST /auth/casback/casLogin                 提交 RSA(PKCS#1 v1.5) 加密后的密码
    │    POST /auth/casback/verifyTgt                用 TGT 换 ST 票据
    ├─ aTrust 网关 (vpn.seu.edu.cn)
    │    GET  /passport/v1/auth/cas?ticket=ST-...    兑换网关会话(sid)
    │    POST /controller/v1/public/reportEnv        上报浏览器设备(必须, 否则网关不放行)
    │    GET  /passport/v1/auth/authCheck            确认在线, 拿 sidTicket
    ├─ 应用层 CAS + ehall
    │    GET  index.do                               过网关, 拿到 sdp_app_session
    │    POST /auth/casback/verifyTgt (service=ehall) 换应用票据
    │    GET  index.do?ticket=ST-...                 拿到 GS_SESSIONID
    ├─ 数据接口
    │    GET  /gsapp/sys/jzxxtjapp/hdyy/queryActivityList.do   活动(讲座)列表
    │    GET  /gsapp/sys/jzxxtjapp/hdyy/queryMyActivityList.do 我的预约
    │    GET  /gsapp/sys/jzxxtjapp/hdyy/queryNotice.do         通知公告
    │    GET  /gsapp/sys/jzxxtjapp/hdyy/getSetting.do          违约规则/说明
    └─ 预约 / 取消预约
         POST /gsapp/sys/jzxxtjapp/hdyy/appiontCheck.do   wid=<WID>        预约前置校验(只读)
         GET  /gsapp/sys/jzxxtjapp/hdyy/vcode.do          _=<时间戳>       4 位图形验证码
         POST /gsapp/sys/jzxxtjapp/hdyy/yySave.do         paramJson={"HD_WID":"..","vcode":"1234"}
         POST /gsapp/sys/jzxxtjapp/hdyy/delete.do         hdyyJson={"HD_WID":".."}

     注意: 预约必须先过图形验证码(服务端先校验验证码, 再校验活动是否存在), 取消预约不需要验证码。
     实测: 验证码有效期约 1 分钟, 取图后要立刻提交; 传空验证码返回「非法参数」。
     yySave 成功返回 {"code":200,"success":true}; delete 成功返回 {"success":true}。

依赖: pip install requests        (RSA 加密用纯 Python 实现, 无需 cryptography)
      pip install ddddocr         (可选, 仅 --ocr 自动识别验证码时需要)

用法:
    export SEU_USERNAME=你的学号
    export SEU_PASSWORD='你的密码'

    python seu_lecture_proto.py                  # 打印讲座列表
    python seu_lecture_proto.py --detail         # 每场讲座的完整信息
    python seu_lecture_proto.py --notice --my    # 追加通知公告 / 我的预约
    python seu_lecture_proto.py --json out.json  # 保存原始 JSON
    python seu_lecture_proto.py --no-cache       # 忽略 Cookie 缓存, 强制重新登录
    python seu_lecture_proto.py --debug          # 打印每一步请求的细节

    python seu_lecture_proto.py --check 2              # 只做第 2 场的预约前置校验
    python seu_lecture_proto.py --reserve 2            # 预约第 2 场(会取验证码并提示输入)
    python seu_lecture_proto.py --reserve 2 --ocr      # 用 ddddocr 自动识别验证码, 无需人工输入
    python seu_lecture_proto.py --reserve 2 --vcode 6013   # 直接给验证码, 便于脚本调用
    python seu_lecture_proto.py --cancel 1             # 取消我的第 1 条预约
    python seu_lecture_proto.py --reserve 2 --dry-run  # 只打印要发的请求, 不提交
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import random
import secrets
import sys
import time
import unicodedata
import uuid
from datetime import datetime
from pathlib import Path
from typing import Any, Callable, Iterable
from urllib.parse import parse_qs, urljoin, urlparse

try:
    import requests
except ImportError:  # pragma: no cover
    sys.exit("缺少 requests 库, 请先执行: pip install requests")


# --------------------------------------------------------------------------- #
# 常量
# --------------------------------------------------------------------------- #

AUTH = "https://auth.seu.edu.cn"
PORTAL = "https://vpn.seu.edu.cn"

CAS_SERVICE = f"{PORTAL}:443/passport/v1/auth/cas?sfDomain=CAS-auth"
APP_SERVICE = "http://ehall.seu.edu.cn/gsapp/sys/jzxxtjapp/*default/index.do"
APP_URL = "https://ehall.seu.edu.cn/gsapp/sys/jzxxtjapp/*default/index.do"

API_BASE = "https://ehall.seu.edu.cn/gsapp/sys/jzxxtjapp/hdyy/"
EP_ACTIVITY = API_BASE + "queryActivityList.do"
EP_MY = API_BASE + "queryMyActivityList.do"
EP_NOTICE = API_BASE + "queryNotice.do"
EP_CHECK = API_BASE + "appiontCheck.do"     # 预约前置校验
EP_VCODE = API_BASE + "vcode.do"            # 预约图形验证码
EP_YY_SAVE = API_BASE + "yySave.do"         # 提交预约
EP_DELETE = API_BASE + "delete.do"          # 取消预约

PORTAL_PARAMS = {"clientType": "SDPBrowserClient", "platform": "Linux", "lang": "zh-CN"}
UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36")

DEFAULT_CACHE = Path.home() / ".seu_lecture_proto_session.json"


class LoginError(RuntimeError):
    """登录流程失败。"""


class SessionExpired(RuntimeError):
    """缓存的会话已失效。"""


# --------------------------------------------------------------------------- #
# RSA: PKCS#1 v1.5 加密 (纯 Python, 无需第三方库)
# --------------------------------------------------------------------------- #

def _der_tlv(data: bytes, pos: int = 0) -> tuple[int, bytes, int]:
    """读取一个 DER TLV, 返回 (tag, value, next_pos)。"""
    tag = data[pos]
    pos += 1
    length = data[pos]
    pos += 1
    if length & 0x80:
        count = length & 0x7F
        length = int.from_bytes(data[pos:pos + count], "big")
        pos += count
    return tag, data[pos:pos + length], pos + length


def parse_public_key(pem_or_b64: str) -> tuple[int, int]:
    """解析 SubjectPublicKeyInfo, 返回 (n, e)。支持 PEM 与 base64url 裸 DER。"""
    text = pem_or_b64.strip()
    if "BEGIN" in text:
        b64 = "".join(line for line in text.splitlines() if "-----" not in line)
        der = base64.b64decode(b64)
    else:
        der = base64.urlsafe_b64decode(text + "=" * (-len(text) % 4))

    _, outer, _ = _der_tlv(der, 0)            # SEQUENCE
    _, _, pos = _der_tlv(outer, 0)            # AlgorithmIdentifier
    _, bits, _ = _der_tlv(outer, pos)         # BIT STRING
    _, seq, _ = _der_tlv(bits[1:], 0)         # RSAPublicKey
    _, n_raw, pos = _der_tlv(seq, 0)
    _, e_raw, _ = _der_tlv(seq, pos)
    return int.from_bytes(n_raw, "big"), int.from_bytes(e_raw, "big")


def rsa_encrypt(public_key: str, plaintext: str) -> str:
    """RSA/PKCS#1 v1.5 加密后 base64 编码, 等价于前端的 JSEncrypt。"""
    n, e = parse_public_key(public_key)
    size = (n.bit_length() + 7) // 8
    message = plaintext.encode("utf-8")
    if len(message) > size - 11:
        raise ValueError("密码过长, RSA 无法填充")
    padding = bytes(random.randint(1, 255) for _ in range(size - 3 - len(message)))
    block = b"\x00\x02" + padding + b"\x00" + message
    return base64.b64encode(pow(int.from_bytes(block, "big"), e, n).to_bytes(size, "big")).decode()


# --------------------------------------------------------------------------- #
# 显示宽度处理(中文占两格, 保证表格对齐)
# --------------------------------------------------------------------------- #

def disp_width(text: str) -> int:
    return sum(2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1 for ch in str(text))


def shorten(text: str, max_width: int) -> str:
    text = str(text)
    if disp_width(text) <= max_width:
        return text
    out, width = "", 0
    for ch in text:
        w = 2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1
        if width + w > max_width - 1:
            break
        out += ch
        width += w
    return out + "…"


def pad(text: str, width: int, align: str = "left") -> str:
    text = str(text)
    gap = max(0, width - disp_width(text))
    if align == "right":
        return " " * gap + text
    if align == "center":
        left = gap // 2
        return " " * left + text + " " * (gap - left)
    return text + " " * gap


# --------------------------------------------------------------------------- #
# 协议客户端
# --------------------------------------------------------------------------- #

class SeuLectureClient:
    """完成 CAS + aTrust + ehall 登录并调用数据接口。"""

    def __init__(self, username: str, password: str, debug: bool = False, timeout: float = 30.0):
        self.username = username
        self.password = password
        self.debug = debug
        self.timeout = timeout
        self.session = requests.Session()
        self.session.headers.update({"User-Agent": UA, "Accept-Language": "zh-CN,zh;q=0.9"})
        self.user_info: dict[str, Any] = {}
        self._csrf = str(uuid.uuid4())
        self._portal_referer = PORTAL + "/portal/shortcut.html"

    # ---------------------------- 工具 ---------------------------- #

    def log(self, message: str, always: bool = False) -> None:
        if always or self.debug:
            print(message, flush=True)

    def _request(self, method: str, url: str, **kwargs: Any) -> requests.Response:
        kwargs.setdefault("timeout", self.timeout)
        kwargs.setdefault("allow_redirects", False)
        if self.debug:
            print(f"    {method} {url[:150]}", flush=True)
        resp = self.session.request(method, url, **kwargs)
        if self.debug:
            print(f"      -> {resp.status_code} {len(resp.content)}B", flush=True)
        return resp

    def _json(self, method: str, url: str, **kwargs: Any) -> dict[str, Any]:
        resp = self._request(method, url, **kwargs)
        try:
            data = resp.json()
        except ValueError as exc:
            raise LoginError(f"接口返回的不是 JSON: {url}\n{resp.text[:200]}") from exc
        if self.debug:
            print(f"      body {json.dumps(data, ensure_ascii=False)[:400]}", flush=True)
        return data

    # ---------------------------- 登录 ---------------------------- #

    def login(self) -> None:
        """完整登录流程。"""
        self._cas_login()
        self._portal_login()
        self._app_login()

    def _cas_login(self) -> None:
        """统一身份认证: RSA 加密密码换取 CAS 会话。"""
        self.log("[1/3] 统一身份认证 (CAS) ...", always=True)
        self.session.headers["Origin"] = AUTH
        self.session.headers["Referer"] = AUTH + "/dist/"
        self._request("GET", AUTH + "/dist/")
        self._json("POST", AUTH + "/auth/casback/verifyTgt", json={"service": CAS_SERVICE})

        captcha = self._json("GET", AUTH + "/auth/casback/needCaptcha")
        if captcha.get("needStage2Validation"):
            raise LoginError("该账号需要二次校验(手机验证码等), 纯协议版无法完成, 请改用浏览器版")
        if captcha.get("code") not in (200, None):
            raise LoginError(f"认证前置检查失败: {captcha.get('info')}")

        key = self._json("POST", AUTH + "/auth/casback/getChiperKey", json={})
        public_key = key.get("publicKey")
        if not public_key:
            raise LoginError(f"获取加密公钥失败: {key}")

        payload = {
            "service": CAS_SERVICE,
            "username": self.username,
            "password": rsa_encrypt(public_key, self.password),
            "captcha": "",
            "rememberMe": False,
            "loginType": "account",
            "wxBinded": False,
            "mobilePhoneNum": "",
            "fingerPrint": "stable_" + hashlib.md5(self.username.encode()).hexdigest(),
        }
        result = self._json("POST", AUTH + "/auth/casback/casLogin", json=payload)
        if not result.get("success"):
            raise LoginError(f"账号或密码错误: {result.get('info')} (code={result.get('code')})")
        if result.get("needStage2Validation"):
            raise LoginError("登录后要求二次校验, 纯协议版无法完成")
        self.log("      密码校验通过, 已获得 CAS TGT")

    def _portal_login(self) -> None:
        """aTrust 网关: 换票据 + 上报设备 + 确认在线。"""
        self.log("[2/3] aTrust 网关会话 ...", always=True)
        self.session.headers.pop("Origin", None)      # 网关接口不认 CAS 的 Origin
        ticket_info = self._json("POST", AUTH + "/auth/casback/verifyTgt", json={"service": CAS_SERVICE})
        redirect = ticket_info.get("redirectUrl")
        if not redirect:
            raise LoginError(f"CAS 未下发网关票据: {ticket_info.get('info')}")

        resp = self._request("GET", redirect)
        location = resp.headers.get("Location", "")
        if not location:
            raise LoginError(f"网关票据兑换失败, 状态码 {resp.status_code}")

        # 门户页地址里带着这张会话对应的 ticket, reportEnv 必须用它
        query = parse_qs(urlparse(location).query)
        try:
            portal_data = json.loads(query["data"][0])
            sid_ticket = portal_data["ticket"]
        except (KeyError, IndexError, ValueError) as exc:
            raise LoginError(f"网关返回的门户地址无法解析: {location[:200]}") from exc

        self._portal_referer = urljoin(PORTAL, location)
        self._request("GET", self._portal_referer)

        headers = {
            "x-csrf-token": self._csrf,
            "x-sdp-traceid": secrets.token_hex(4),
            "Referer": self._portal_referer,
        }
        device_id = "00" + secrets.token_hex(33)
        self._request(
            "POST", PORTAL + "/controller/v1/public/reportEnv", params=PORTAL_PARAMS, headers=headers,
            json={"ticket": sid_ticket, "deviceId": device_id,
                  "env": {"endpoint": {"device_id": device_id, "device": {"type": "browser"}}}},
        )
        check = self._json("GET", PORTAL + "/passport/v1/auth/authCheck",
                           params=PORTAL_PARAMS, headers=headers)
        if check.get("code") != 0:
            raise LoginError(f"网关在线校验失败: {check.get('message')}")
        self.user_info = (check.get("data") or {}).get("onlineInfo") or {}
        name = self.user_info.get("displayName") or ""
        self.log(f"      网关已放行: {self.user_info.get('username', self.username)} {name}".rstrip())

    def _app_login(self) -> None:
        """过网关访问 ehall, 再用应用层 CAS 票据换取 ehall 会话。"""
        self.log("[3/3] ehall 应用登录 ...", always=True)
        resp = self._request("GET", APP_URL)
        for _ in range(6):
            if resp.status_code not in (301, 302, 303, 307):
                break
            location = urljoin(resp.url, resp.headers.get("Location", ""))
            if "auth.seu.edu.cn" in location:
                break                       # 应用层 CAS 登录页, 交给下面的换票
            if "/portal/shortcut.html" in location and "login" in location:
                raise LoginError("网关未放行(仍被重定向到门户登录页), 请稍后重试")
            resp = self._request("GET", location)

        ticket_info = self._json("POST", AUTH + "/auth/casback/verifyTgt", json={"service": APP_SERVICE})
        redirect = ticket_info.get("redirectUrl")
        if not redirect:
            raise LoginError(f"应用票据申请失败: {ticket_info.get('info')}")

        resp = self._request("GET", redirect, allow_redirects=True)
        if resp.status_code != 200:
            raise LoginError(f"ehall 登录失败, 状态码 {resp.status_code}")
        names = {c.name for c in self.session.cookies}
        if "GS_SESSIONID" not in names:
            raise LoginError(f"没有拿到 ehall 会话 Cookie, 当前: {sorted(names)}")
        self.log("      已获得 ehall 会话")

    # ---------------------------- 数据接口 ---------------------------- #

    def api(self, url: str, **params: Any) -> dict[str, Any]:
        resp = self.session.get(url, params=params, timeout=self.timeout, allow_redirects=False)
        if resp.status_code in (301, 302, 303, 307) or not resp.text.lstrip().startswith("{"):
            raise SessionExpired("会话已失效")
        return resp.json()

    def fetch_all(self, url: str, page_size: int = 50) -> list[dict[str, Any]]:
        items: list[dict[str, Any]] = []
        seen: set[str] = set()
        page = 1
        while page <= 50:
            data = self.api(url, pageIndex=page, pageSize=page_size)
            rows = data.get("datas") or []
            for row in rows:
                key = str(row.get("WID") or row.get("HD_WID") or json.dumps(row, sort_keys=True))
                if key not in seen:
                    seen.add(key)
                    items.append(row)
            total = int(data.get("total") or 0)
            if not rows or len(items) >= total:
                break
            page += 1
        return items

    # ------------------------- 预约 / 取消预约 ------------------------- #

    def post_api(self, url: str, **data: Any) -> dict[str, Any]:
        """业务接口都是表单提交 (jQuery $.ajax 的默认编码)。"""
        resp = self.session.post(url, data=data, timeout=self.timeout, allow_redirects=False)
        if resp.status_code in (301, 302, 303, 307) or not resp.text.lstrip().startswith("{"):
            raise SessionExpired("会话已失效")
        return resp.json()

    def appoint_check(self, wid: str) -> dict[str, Any]:
        """预约前置校验, 只读。返回 {success, msg}。"""
        return self.post_api(EP_CHECK, wid=wid)

    def fetch_vcode(self, out_path: Path) -> Path:
        """取预约用的图形验证码, 保存为 jpg 并返回路径。"""
        data = self.api(EP_VCODE, _=int(datetime.now().timestamp() * 1000))
        if not data.get("success"):
            raise LoginError(f"获取验证码失败: {data}")
        raw = str(data.get("result") or "")
        b64 = raw.split(",", 1)[-1]
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_bytes(base64.b64decode(b64))
        return out_path

    def reserve(self, wid: str, vcode: str) -> dict[str, Any]:
        """提交预约。返回 {success, msg}。"""
        payload = json.dumps({"HD_WID": wid, "vcode": vcode}, ensure_ascii=False)
        return self.post_api(EP_YY_SAVE, paramJson=payload)

    def cancel(self, wid: str) -> dict[str, Any]:
        """取消预约。wid 为活动 WID (即列表里的 WID / 我的预约里的 HD_WID)。"""
        payload = json.dumps({"HD_WID": wid}, ensure_ascii=False)
        return self.post_api(EP_DELETE, hdyyJson=payload)


# --------------------------------------------------------------------------- #
# 数据展示
# --------------------------------------------------------------------------- #

def _num(value: Any) -> int:
    try:
        return int(str(value).strip())
    except (TypeError, ValueError):
        return 0


def _parse_time(value: Any) -> datetime | None:
    if not value:
        return None
    text = str(value).strip()
    for fmt in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d %H:%M", "%Y-%m-%d"):
        try:
            return datetime.strptime(text, fmt)
        except ValueError:
            continue
    return None


def lecture_status(item: dict[str, Any], now: datetime) -> str:
    """与网页前端 hdlb 模块一致的预约状态推导。"""
    if str(item.get("FBZT")) == "-1":
        return "已取消"
    if item.get("YY_WID"):
        if str(item.get("SFWY")) == "1":
            return "已违约"
        if str(item.get("SFDK")) == "1":
            return "已打卡"
        return "已预约"
    start = _parse_time(item.get("YYKSSJ"))
    end = _parse_time(item.get("YYJSSJ"))
    if now and start and now < start:
        return "未开放"
    if now and end and now > end:
        return "已结束"
    total, used = _num(item.get("HDZRS")), _num(item.get("YYRS"))
    if total and used >= total:
        return "已满"
    return "可预约"


def _fmt_range(item: dict[str, Any]) -> str:
    start = str(item.get("JZSJ") or "")[:16]
    end_raw = str(item.get("HDJSSJ") or "")
    same_day = end_raw[:10] == str(item.get("JZSJ") or "")[:10]
    end = end_raw[11:16] if same_day and len(end_raw) >= 16 else end_raw[:16]
    return f"{start} ~ {end}" if end else start


def print_lectures(items: list[dict[str, Any]], detail: bool = False) -> None:
    if not items:
        print("没有查到讲座信息。")
        return

    now = _parse_time(items[0].get("XTDQSJ")) or datetime.now()
    items = sorted(items, key=lambda x: str(x.get("JZSJ") or ""))

    print("=" * 100)
    print(f" 东南大学 · 素质讲座 / 活动预约    共 {len(items)} 场    "
          f"(服务器时间 {now:%Y-%m-%d %H:%M:%S})")
    print("=" * 100)

    if detail:
        for idx, item in enumerate(items, 1):
            print(f"\n[{idx}] {item.get('JZMC', '')}")
            print(f"    时间    : {_fmt_range(item)}")
            print(f"    地点    : {item.get('JZDD', '') or '-'}")
            print(f"    主讲人  : {item.get('ZJR', '') or '-'}")
            print(f"    类别    : {item.get('JZXL_DISPLAY', '') or item.get('JZXL', '') or '-'}")
            print(f"    主办    : {item.get('ZBF', '') or '-'}")
            print(f"    预约    : {_num(item.get('YYRS'))}/{_num(item.get('HDZRS'))} 人"
                  f"    状态: {lecture_status(item, now)}")
            window = f"{str(item.get('YYKSSJ') or '')[:16]} ~ {str(item.get('YYJSSJ') or '')[:16]}"
            print(f"    预约窗口: {window.strip(' ~') or '-'}")
            if item.get("JZJS"):
                print(f"    简介    : {shorten(str(item['JZJS']).replace(chr(10), ' '), 96)}")
            if item.get("ZJRJS"):
                print(f"    主讲简介: {shorten(str(item['ZJRJS']).replace(chr(10), ' '), 96)}")
        return

    headers = ["#", "讲座时间", "地点", "预约", "状态", "讲座名称"]
    widths = [4, 24, 22, 9, 8, 48]
    line = "  ".join(pad(h, w) for h, w in zip(headers, widths))
    print(line)
    print("-" * disp_width(line))
    for idx, item in enumerate(items, 1):
        cells = [
            pad(str(idx), widths[0], "right"),
            pad(_fmt_range(item), widths[1]),
            pad(shorten(item.get("JZDD", "") or "-", widths[2]), widths[2]),
            pad(f"{_num(item.get('YYRS'))}/{_num(item.get('HDZRS'))}", widths[3], "right"),
            pad(lecture_status(item, now), widths[4]),
            pad(shorten(item.get("JZMC", ""), widths[5]), widths[5]),
        ]
        print("  ".join(cells))


def print_notices(items: list[dict[str, Any]]) -> None:
    if not items:
        print("\n没有通知公告。")
        return
    print("\n" + "=" * 100)
    print(f" 通知公告    共 {len(items)} 条")
    print("=" * 100)
    for idx, item in enumerate(items, 1):
        print(f"\n[{idx}] {item.get('BT', '(无标题)')}    ({item.get('FBSJ', '')})")
        body = str(item.get("NR") or "").replace("\n", " ").strip()
        print(f"    {shorten(body, 300)}")


def print_my(items: list[dict[str, Any]]) -> None:
    if not items:
        print("\n我的预约: 暂无预约记录。")
        return
    print("\n" + "=" * 100)
    print(f" 我的预约    共 {len(items)} 条")
    print("=" * 100)
    for idx, item in enumerate(items, 1):
        flag = "已打卡" if str(item.get("SFDK")) == "1" else ("已违约" if str(item.get("SFWY")) == "1" else "已预约")
        print(f"\n[{idx}] {item.get('JZMC', '')}  [{flag}]")
        print(f"    时间: {_fmt_range(item)}    地点: {item.get('JZDD', '') or '-'}")
        print(f"    预约时间: {item.get('YYSJ', '')}    预约码: {item.get('YYM', '') or '-'}")
        print(f"    WID : {item.get('HD_WID', '')}")


# --------------------------------------------------------------------------- #
# 预约 / 取消预约
# --------------------------------------------------------------------------- #

def preview_captcha(path: Path) -> None:
    """有 PIL 时把验证码画成字符图, 方便在终端里直接看。"""
    try:
        from PIL import Image, ImageOps
    except ImportError:
        return
    try:
        img = Image.open(path).convert("L")
        width, height = img.size
        img = ImageOps.autocontrast(img.crop((2, 3, width - 2, height - 3)))
        pixels = img.load()
        width, height = img.size
        lines = []
        for y in range(0, height - 1, 2):
            line = ""
            for x in range(width):
                top = pixels[x, y] < 140
                bottom = pixels[x, y + 1] < 140
                line += "█" if top and bottom else ("▀" if top else ("▄" if bottom else " "))
            lines.append(line.rstrip())
        print("\n".join(lines))
    except Exception:
        pass


def _pick(items: list[dict[str, Any]], index: int, what: str) -> dict[str, Any]:
    if not items:
        raise LoginError(f"{what}列表为空")
    if not 1 <= index <= len(items):
        raise LoginError(f"{what}序号 {index} 超出范围 (1-{len(items)})")
    return items[index - 1]


def _resolve(value: str, items: list[dict[str, Any]], key: str, what: str) -> tuple[str, str]:
    """参数既可以是列表序号, 也可以是 WID。返回 (wid, 标题)。"""
    if not items:
        raise LoginError(f"{what}列表为空")
    text = str(value).strip()
    if text.isdigit() and len(text) <= 4:
        item = _pick(items, int(text), what)
    else:
        item = next((it for it in items if str(it.get(key)) == text), None)
        if item is None:
            raise LoginError(f"{what}列表里没有 WID={text}")
    return str(item.get(key)), str(item.get("JZMC", ""))


def _confirm(args: argparse.Namespace, prompt: str) -> bool:
    if args.yes:
        return True
    try:
        answer = input(f"{prompt} [y/N] ").strip().lower()
    except EOFError:
        return False
    return answer in ("y", "yes", "是")


_OCR_ENGINE = None


def ocr_read(path: Path) -> str:
    """用 ddddocr 识别验证码图片。模块是懒加载的, 没装也不影响其它功能。"""
    global _OCR_ENGINE
    try:
        import ddddocr
    except ImportError:
        raise LoginError("未安装 ddddocr, 请先执行: pip install ddddocr") from None
    if _OCR_ENGINE is None:
        _OCR_ENGINE = ddddocr.DdddOcr(show_ad=False)
    with open(path, "rb") as handle:
        return str(_OCR_ENGINE.classification(handle.read())).strip()


def _ask_vcode(client: SeuLectureClient, args: argparse.Namespace) -> str:
    if args.vcode:
        return args.vcode
    path = Path(args.vcode_out).expanduser()
    client.fetch_vcode(path)
    print(f"\n[*] 验证码已保存: {path.resolve()}")

    if args.ocr:
        guess = ocr_read(path)
        print(f"[*] ddddocr 识别结果: {guess!r}")
        if not guess:
            raise LoginError("ddddocr 没有识别出内容")
        return guess

    preview_captcha(path)
    print("(验证码约 1 分钟内有效, 请尽快输入)")
    try:
        return input("请输入图中 4 位验证码 (直接回车退出): ").strip()
    except EOFError:
        raise LoginError("当前不是交互式终端, 请用 --vcode 传入验证码, 或加 --ocr") from None


def do_action(client: SeuLectureClient, args: argparse.Namespace) -> int:
    """处理 --check / --reserve / --cancel。"""
    action = "cancel" if args.cancel is not None else ("reserve" if args.reserve is not None else "check")

    if action == "cancel":
        mine = sorted(client.fetch_all(EP_MY, args.page_size), key=lambda x: str(x.get("JZSJ") or ""))
        print_my(mine)
        wid, title = _resolve(args.wid or args.cancel, mine, "HD_WID", "预约记录")
    else:
        lectures = sorted(client.fetch_all(EP_ACTIVITY, args.page_size),
                          key=lambda x: str(x.get("JZSJ") or ""))
        print_lectures(lectures)
        wid, title = _resolve(args.wid or args.reserve or args.check, lectures, "WID", "活动")

    if action == "check":
        result = client.appoint_check(wid)
        print(f"\n[*] 预检结果: {json.dumps(result, ensure_ascii=False)}")
        return 0 if result.get("success") else 1

    if action == "cancel":
        request_desc = {"POST": EP_DELETE, "hdyyJson": json.dumps({"HD_WID": wid}, ensure_ascii=False)}
        verb = "取消预约"
    else:
        request_desc = {"POST": EP_YY_SAVE,
                        "paramJson": json.dumps({"HD_WID": wid, "vcode": "<4位验证码>"}, ensure_ascii=False)}
        verb = "预约"

    if args.dry_run:
        print(f"\n[*] 将要{verb}: {title}")
        print(json.dumps(request_desc, ensure_ascii=False, indent=1))
        return 0

    if not _confirm(args, f"\n确认{verb}「{title}」?"):
        print("[*] 已放弃")
        return 0

    if action == "cancel":
        result = client.cancel(wid)
    else:
        # 验证码有效期很短(实测约 1 分钟), 所以取图后要立刻提交; 错了就重新取一张
        result = {}
        attempts = 3 if (args.ocr or (sys.stdin.isatty() and not args.vcode)) else 1
        for attempt in range(attempts):
            vcode = _ask_vcode(client, args)
            if not vcode:
                print("[*] 未输入验证码, 已放弃")
                return 0
            result = client.reserve(wid, vcode)
            message = str(result.get("msg", ""))
            if result.get("success") or "验证码" not in message:
                if "频繁" in message:
                    print("[!] 触发风控限流, 请等几分钟再试")
                break
            if attempt < attempts - 1:
                print(f"[!] {result.get('msg')}\n[*] 已重新获取验证码, 请再试一次 "
                      f"(还剩 {attempts - 1 - attempt} 次)")
                if args.ocr:
                    time.sleep(3)      # 别打太快, 服务端有风控
                args.vcode = None      # 让下一轮重新取图

    success = bool(result.get("success"))
    print(f"\n{'[+]' if success else '[!]'} {verb}{'成功' if success else '失败'}"
          f"{'' if success else ': ' + str(result.get('msg') or result)}")
    return 0 if success else 1


# --------------------------------------------------------------------------- #
# Cookie 缓存
# --------------------------------------------------------------------------- #

def load_cache(path: Path) -> list[dict[str, Any]] | None:
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
        return raw["cookies"]
    except Exception:
        return None


def save_cache(path: Path, cookies: Iterable[Any], user_info: dict[str, Any]) -> None:
    try:
        path.write_text(
            json.dumps(
                {
                    "cookies": [{"name": c.name, "value": c.value, "domain": c.domain, "path": c.path}
                                for c in cookies],
                    "user_info": user_info,
                    "saved_at": datetime.now().isoformat(timespec="seconds"),
                },
                ensure_ascii=False, indent=1,
            ),
            encoding="utf-8",
        )
        path.chmod(0o600)
    except Exception as exc:
        print(f"[!] 保存 Cookie 缓存失败: {exc}", file=sys.stderr)


def apply_cache(client: SeuLectureClient, cookies: Iterable[dict[str, Any]]) -> None:
    for cookie in cookies:
        client.session.cookies.set(
            cookie["name"], cookie["value"],
            domain=cookie.get("domain") or "ehall.seu.edu.cn", path=cookie.get("path") or "/",
        )


# --------------------------------------------------------------------------- #
# 命令行
# --------------------------------------------------------------------------- #

def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="东南大学素质讲座(活动预约)信息抓取 —— 纯协议版, 不启动浏览器",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("-u", "--username", default=os.environ.get("SEU_USERNAME"),
                        help="一卡通号/学号 (默认读环境变量 SEU_USERNAME)")
    parser.add_argument("-p", "--password", default=os.environ.get("SEU_PASSWORD"),
                        help="密码 (默认读环境变量 SEU_PASSWORD)")
    parser.add_argument("--page-size", type=int, default=50, help="每页条数, 默认 50")
    parser.add_argument("--detail", action="store_true", help="打印每场讲座的完整信息")
    parser.add_argument("--notice", action="store_true", help="额外打印通知公告")
    parser.add_argument("--my", action="store_true", help="额外打印我的预约记录")
    parser.add_argument("--json", dest="json_path", help="把原始数据保存为 JSON 文件")
    parser.add_argument("--cache-file", default=str(DEFAULT_CACHE), help="Cookie 缓存文件路径")
    parser.add_argument("--no-cache", action="store_true", help="不使用本地 Cookie 缓存")
    parser.add_argument("--debug", action="store_true", help="打印每一步请求细节")
    parser.add_argument("--timeout", type=float, default=30.0, help="单次请求超时秒数, 默认 30")

    act = parser.add_argument_group("预约 / 取消预约")
    act.add_argument("--check", metavar="N|WID", help="只做预约前置校验(只读), 传序号或活动 WID")
    act.add_argument("--reserve", metavar="N|WID", help="预约活动, 传列表序号或活动 WID")
    act.add_argument("--cancel", metavar="N|WID", help="取消预约, 传我的预约序号或活动 WID")
    act.add_argument("--wid", help="直接指定活动 WID, 配合 --check/--reserve/--cancel 使用")
    act.add_argument("--vcode", help="直接提供验证码(跳过取图), 便于脚本化调用")
    act.add_argument("--ocr", action="store_true", help="用 ddddocr 自动识别验证码(需 pip install ddddocr)")
    act.add_argument("--vcode-out", default="vcode.jpg", help="验证码图片保存路径, 默认 vcode.jpg")
    act.add_argument("--yes", action="store_true", help="跳过确认提示")
    act.add_argument("--dry-run", action="store_true", help="只打印将要发送的请求, 不真正提交")

    args = parser.parse_args(argv)
    if not args.username or not args.password:
        parser.error("缺少账号或密码: 请用 -u/-p 指定, 或设置 SEU_USERNAME / SEU_PASSWORD 环境变量")
    if args.wid and args.check is None and args.reserve is None and args.cancel is None:
        parser.error("--wid 需要配合 --check / --reserve / --cancel 使用")
    return args


def build_client(args: argparse.Namespace) -> SeuLectureClient:
    """优先复用缓存 Cookie, 失效则完整重新登录。"""
    cache_path = Path(args.cache_file).expanduser()

    if not args.no_cache:
        cached = load_cache(cache_path)
        if cached:
            print("[*] 尝试复用本地 Cookie ...", flush=True)
            client = SeuLectureClient(args.username, args.password, args.debug, args.timeout)
            apply_cache(client, cached)
            try:
                client.api(EP_ACTIVITY, pageIndex=1, pageSize=1)
                print("[*] Cookie 仍然有效, 免登录")
                return client
            except (SessionExpired, requests.RequestException, ValueError):
                print("[*] Cookie 已失效, 重新登录")

    client = SeuLectureClient(args.username, args.password, args.debug, args.timeout)
    client.login()
    save_cache(cache_path, client.session.cookies, client.user_info)
    print(f"[*] 已保存登录状态到 {cache_path}")
    return client


def run(client: SeuLectureClient, args: argparse.Namespace) -> int:
    """抓取并打印; 会话中途失效时抛 SessionExpired 交给外层重登。"""
    lectures = client.fetch_all(EP_ACTIVITY, args.page_size)
    print_lectures(lectures, detail=args.detail)

    notices = client.fetch_all(EP_NOTICE, args.page_size) if args.notice else []
    if args.notice:
        print_notices(notices)

    mine = client.fetch_all(EP_MY, args.page_size) if args.my else []
    if args.my:
        print_my(mine)

    if args.json_path:
        payload = {
            "fetched_at": datetime.now().isoformat(timespec="seconds"),
            "user": client.user_info.get("username", args.username),
            "lectures": lectures,
        }
        if args.notice:
            payload["notices"] = notices
        if args.my:
            payload["my_activities"] = mine
        out_path = Path(args.json_path).expanduser()
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        print(f"\n[*] 原始数据已保存: {out_path.resolve()}")
    return 0


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = build_client(args)

    # 有预约/取消动作时只执行动作, 不再打印整张列表
    runner: Callable[[SeuLectureClient, argparse.Namespace], int] = (
        do_action if (args.check is not None or args.reserve is not None or args.cancel is not None) else run
    )

    try:
        return runner(client, args)
    except SessionExpired as exc:
        # 缓存/中途失效: 自动重新登录一次再重试
        print(f"[*] 会话失效({exc}), 自动重新登录 ...", file=sys.stderr)
        try:
            client = SeuLectureClient(args.username, args.password, args.debug, args.timeout)
            client.login()
            save_cache(Path(args.cache_file).expanduser(), client.session.cookies, client.user_info)
            return runner(client, args)
        except LoginError:
            raise
        except SessionExpired:
            print("[!] 重新登录后仍然失效, 请稍后再试", file=sys.stderr)
            return 2
    except requests.RequestException as exc:
        print(f"[!] 网络请求失败: {exc}", file=sys.stderr)
        return 3


if __name__ == "__main__":
    try:
        sys.exit(main())
    except LoginError as exc:
        print(f"[!] {exc}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("\n[*] 已取消", file=sys.stderr)
        sys.exit(130)