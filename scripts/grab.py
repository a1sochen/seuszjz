#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
东南大学素质讲座「自动抢座」交互式脚本

流程:
    1. 自动登录(账号密码可写死在下面, 也可用 -u/-p 指定);
    2. 拉取并打印讲座列表, 等你选择要抢哪一场;
    3. 询问两个节奏参数: 目标"已满/未开放"时多久重试、被限流时等多久;
    4. 进入抢座循环:
         目标可约 -> 取验证码 -> ddddocr 识别 -> 只提交 4 位纯数字 ->
         成功则打印预约码并退出; 验证码错就立刻换一张; 被限流就按你设的时间等待。

验证码策略(按你的要求):
    - OCR 结果不是 4 位纯数字时, 不提交, 直接重新取图再识别;
    - 识别/提交失败一直重试;
    - 失败次数多被服务端限制时, 按你给的秒数等待后继续。

依赖:
    pip install requests ddddocr
    同目录下需要 seu_lecture_proto.py (复用其中的登录与接口实现)。

用法:
    python seu_lecture_grab.py                 # 用脚本里写死的账号
    python seu_lecture_grab.py -u 学号 -p 密码  # 临时指定账号

注意: 抢到后如果不去参加, 按学校规则会记违约, 请自行权衡。
"""

from __future__ import annotations

import argparse
import getpass
import os
import re
import sys
import tempfile
import time
from datetime import datetime
from pathlib import Path

try:
    import requests
except ImportError:
    sys.exit("缺少 requests 库, 请先执行: pip install requests")

try:
    from seu_lecture_proto import (
        EP_ACTIVITY,
        EP_MY,
        LoginError,
        SeuLectureClient,
        SessionExpired,
        _num,
        _parse_time,
        lecture_status,
        print_lectures,
    )
except ImportError:
    sys.exit("需要同目录下的 seu_lecture_proto.py, 请把它和本脚本放在一起")

try:
    import ddddocr
except ImportError:
    sys.exit("本脚本用 ddddocr 识别验证码, 请先执行: pip install ddddocr")


# --------------------------------------------------------------------------- #
# 账号密码: 优先取环境变量, 都没有则运行时交互输入
# --------------------------------------------------------------------------- #

USERNAME = os.environ.get("SEU_USERNAME", "")
PASSWORD = os.environ.get("SEU_PASSWORD", "")

VCODE_PATH = Path(tempfile.gettempdir()) / "seu_vcode_grab.jpg"
OCR = None          # 懒加载的识别引擎


# --------------------------------------------------------------------------- #
# 小工具
# --------------------------------------------------------------------------- #

def ts() -> str:
    return datetime.now().strftime("%H:%M:%S")


def say(message: str) -> None:
    print(f"[{ts()}] {message}", flush=True)


def ask_int(prompt: str, default: int, minimum: int = 0) -> int:
    """交互读取一个整数, 直接回车用默认值。"""
    while True:
        try:
            raw = input(f"{prompt} [默认 {default}]: ").strip()
        except EOFError:
            print()
            return default
        if not raw:
            return default
        try:
            value = int(raw)
        except ValueError:
            print("  请输入整数")
            continue
        if value < minimum:
            print(f"  不能小于 {minimum}")
            continue
        return value


def ask_target(lectures: list[dict]) -> dict:
    """选择要抢的讲座, 支持序号或 WID。"""
    while True:
        try:
            raw = input("\n请输入要抢的讲座序号 (或直接粘贴 WID): ").strip()
        except EOFError:
            sys.exit("\n未选择目标, 已退出")
        if not raw:
            continue
        if raw.isdigit() and len(raw) <= 4:
            index = int(raw)
            if 1 <= index <= len(lectures):
                return lectures[index - 1]
            print(f"  序号需在 1-{len(lectures)} 之间")
            continue
        for item in lectures:
            if str(item.get("WID")) == raw:
                return item
        print("  没有找到这个 WID, 请重新输入")


def ocr_read(path: Path) -> str:
    global OCR
    if OCR is None:
        say("正在加载 ddddocr 模型 ...")
        OCR = ddddocr.DdddOcr(show_ad=False)
    with open(path, "rb") as handle:
        return str(OCR.classification(handle.read())).strip()


def is_four_digits(text: str) -> bool:
    return bool(re.fullmatch(r"[0-9]{4}", text or ""))


# --------------------------------------------------------------------------- #
# 登录 / 列表
# --------------------------------------------------------------------------- #

def get_credentials() -> tuple[str, str]:
    parser = argparse.ArgumentParser(
        description="东南大学素质讲座自动抢座(交互式)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("-u", "--username", help="学号/一卡通号, 不填则用环境变量 SEU_USERNAME 或交互输入")
    parser.add_argument("-p", "--password", help="密码, 不填则用环境变量 SEU_PASSWORD 或交互输入")
    args = parser.parse_args()

    username = args.username or USERNAME or input("学号: ").strip()
    password = args.password or PASSWORD or getpass.getpass("密码: ")
    if not username or not password:
        sys.exit("账号或密码为空")
    return username, password


def login(username: str, password: str) -> SeuLectureClient:
    client = SeuLectureClient(username, password)
    say("正在登录 ...")
    client.login()
    who = client.user_info.get("displayName") or client.user_info.get("username") or username
    say(f"登录成功: {who}")
    return client


def load_lectures(client: SeuLectureClient) -> list[dict]:
    lectures = client.fetch_all(EP_ACTIVITY, 50)
    lectures.sort(key=lambda x: str(x.get("JZSJ") or ""))
    print_lectures(lectures)
    return lectures


def now_of(item: dict) -> datetime:
    return _parse_time(item.get("XTDQSJ")) or datetime.now()


# --------------------------------------------------------------------------- #
# 抢座
# --------------------------------------------------------------------------- #

def reserve_once(client: SeuLectureClient, wid: str, stats: dict) -> tuple[str, str]:
    """取图 -> 识别 -> 只提交 4 位纯数字。返回 (结果, 说明)。

    结果: ok / limited / captcha / other / ocr_fail
    """
    for _ in range(300):
        path = client.fetch_vcode(VCODE_PATH)
        code = ocr_read(path)
        if not is_four_digits(code):
            stats["discarded"] += 1
            if stats["discarded"] % 5 == 1 or stats["discarded"] % 5 == 0:
                say(f"识别结果 {code!r} 不是 4 位数字, 丢弃并重新取图"
                    f"(累计丢弃 {stats['discarded']} 次)")
            time.sleep(0.3)
            continue

        stats["submitted"] += 1
        result = client.reserve(wid, code)
        message = str(result.get("msg") or "")
        if result.get("success"):
            return "ok", code
        if "频繁" in message:
            return "limited", message
        if "验证码" in message:
            stats["wrong_code"] += 1
            say(f"验证码 {code} 服务端不认, 立刻换一张重试"
                f"(累计 {stats['wrong_code']} 次)")
            continue
        return "other", message
    return "ocr_fail", "连续 300 次没拿到可用验证码"


def report_success(client: SeuLectureClient, wid: str) -> None:
    say("抢座成功!")
    try:
        mine = client.fetch_all(EP_MY, 50)
        hit = next((m for m in mine if str(m.get("HD_WID")) == wid), None)
        if hit:
            print(f"    讲座: {hit.get('JZMC')}")
            print(f"    时间: {hit.get('JZSJ')}    地点: {hit.get('JZDD')}")
            print(f"    预约码: {hit.get('YYM')}    预约时间: {hit.get('YYSJ')}")
    except Exception:
        pass


def grab_loop(client: SeuLectureClient, target: dict, interval: int, cooldown: int) -> None:
    wid = str(target.get("WID"))
    title = str(target.get("JZMC"))
    stats = {"submitted": 0, "discarded": 0, "wrong_code": 0, "limited": 0}

    print("\n" + "=" * 90)
    print(f" 目标: {title}")
    print(f"       {target.get('JZSJ')}  {target.get('JZDD')}")
    print(f" 节奏: 已满/未开放 {interval} 秒重试; 被限流等 {cooldown} 秒")
    print("=" * 90 + "\n")

    while True:
        # 1) 先看当前状态, 避免无谓地浪费验证码
        try:
            item = next((a for a in client.fetch_all(EP_ACTIVITY, 50)
                         if str(a.get("WID")) == wid), None)
        except SessionExpired:
            say("会话失效, 重新登录 ...")
            client.login()
            continue

        if item is None:
            say("这场讲座已不在列表里(可能被删除或已结束), 退出")
            return

        status = lecture_status(item, now_of(item))
        if item.get("YY_WID"):
            say("你已经预约过这场讲座了")
            report_success(client, wid)
            return
        if status in ("已结束", "已取消"):
            say(f"当前状态: {status}, 无法再抢, 退出")
            return
        if status in ("已满", "未开放"):
            say(f"当前状态: {status} ({_num(item.get('YYRS'))}/{_num(item.get('HDZRS'))} 人), "
                f"{interval} 秒后再看")
            time.sleep(interval)
            continue

        # 2) 可以预约: 取码 -> 识别 -> 提交
        say(f"发现可预约 (剩余 {_num(item.get('HDZRS')) - _num(item.get('YYRS'))} 个名额), 开始抢")
        outcome, detail = reserve_once(client, wid, stats)

        if outcome == "ok":
            report_success(client, wid)
            print(f"\n本次共提交 {stats['submitted']} 次, "
                  f"丢弃非 4 位识别结果 {stats['discarded']} 次, "
                  f"验证码被拒 {stats['wrong_code']} 次")
            return

        if outcome == "limited":
            stats["limited"] += 1
            say(f"被服务端限流({detail}), 按设置等待 {cooldown} 秒 "
                f"(第 {stats['limited']} 次)")
            time.sleep(cooldown)
            continue

        if outcome == "captcha":
            continue

        say(f"本次未成功: {detail}, {interval} 秒后再试")
        time.sleep(interval)


# --------------------------------------------------------------------------- #
# 入口
# --------------------------------------------------------------------------- #

def main() -> int:
    username, password = get_credentials()
    client = login(username, password)

    try:
        lectures = load_lectures(client)
    except SessionExpired:
        say("会话失效, 重新登录 ...")
        client.login()
        lectures = load_lectures(client)

    if not lectures:
        say("当前没有可显示的讲座, 退出")
        return 0

    target = ask_target(lectures)
    print(f"\n已选择: {target.get('JZMC')}")
    print(f"         {target.get('JZSJ')}   {target.get('JZDD')}")
    print(f"         当前 {_num(target.get('YYRS'))}/{_num(target.get('HDZRS'))} 人, "
          f"预约窗口 {target.get('YYKSSJ')} ~ {target.get('YYJSSJ')}")

    interval = ask_int("\n目标已满或还没开放时, 隔多少秒重试一次?", default=5, minimum=1)
    cooldown = ask_int("被限流(失败太多)时, 等待多少秒再试?", default=300, minimum=5)

    try:
        grab_loop(client, target, interval, cooldown)
    except KeyboardInterrupt:
        print("\n\n已中断")
    except (LoginError, requests.RequestException) as exc:
        print(f"\n[!] {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())