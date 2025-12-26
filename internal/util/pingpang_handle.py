import os
import time
from datetime import datetime, timedelta
from pymongo import MongoClient
from bson.objectid import ObjectId

def _sum_numeric(v):
    if v is None:
        return 0.0
    if isinstance(v, (int, float)):
        return float(v)
    if isinstance(v, dict):
        s = 0.0
        for _, vv in v.items():
            s += _sum_numeric(vv)
        return s
    if isinstance(v, (list, tuple)):
        s = 0.0
        for vv in v:
            s += _sum_numeric(vv)
        return s
    return 0.0

def _load_amf_since(client, db_name, since_dt, supi=None):
    coll = client[db_name]["nwdaf.amf.locationReport"]
    q = {"_id": {"$gt": ObjectId.from_datetime(since_dt)}}
    if supi:
        q["supi"] = supi
    return list(coll.find(q).sort("_id", 1))

def _load_usage_since(client, db_name, since_dt, supi=None):
    coll = client[db_name]["nwdaf.smf.usage"]
    q = {"timestamp": {"$gt": since_dt}}
    if supi:
        q["supi"] = supi
    return list(coll.find(q).sort("timestamp", 1))

def _detect_pingpong(loc_docs, min_toggles):
    cells = []
    for d in loc_docs:
        c = d.get("nrCellId")
        if c is None:
            loc = d.get("location", {})
            nr = (loc.get("nrLocation") or {}).get("ncgi") or {}
            c = nr.get("nrCellId")
        if c is not None:
            cells.append(str(c))
    if len(cells) < 2:
        return None
    uniq = list(dict.fromkeys(cells))
    if len(set(uniq)) != 2:
        return None
    toggles = 0
    prev = cells[0]
    for c in cells[1:]:
        if c != prev:
            toggles += 1
            prev = c
    if toggles >= min_toggles:
        return {"cells": f"{uniq[0]}<->{uniq[1]}", "toggles": toggles}
    return None

def _sum_usage_for_supi(usage_docs, supi):
    total = 0.0
    for d in usage_docs:
        if d.get("supi") != supi:
            continue
        total += _sum_numeric(d.get("usageData"))
    return total

def run(mongo_url, db_name, window_seconds, min_toggles, usage_drop_ratio, poll_seconds, suppress_seconds):
    prev_usage = {}
    last_print = {}
    with MongoClient(mongo_url) as client:
        while True:
            now = datetime.utcnow()
            since = now - timedelta(seconds=window_seconds)
            loc_docs = _load_amf_since(client, db_name, since)
            usage_docs = _load_usage_since(client, db_name, since)
            supis = {d.get("supi") for d in loc_docs if d.get("supi")}
            for supi in supis:
                supi_loc = [d for d in loc_docs if d.get("supi") == supi]
                det = _detect_pingpong(supi_loc, min_toggles)
                if not det:
                    continue
                cur_vol = _sum_usage_for_supi(usage_docs, supi)
                prev = prev_usage.get(supi)
                drop_ok = prev is not None and prev > 0 and cur_vol < prev * (1 - usage_drop_ratio)
                if drop_ok:
                    last = last_print.get(supi)
                    if not last or (now - last).total_seconds() >= suppress_seconds:
                        print(f"检测到UE出现乒乓切换: SUPI={supi}, Cells={det['cells']}, 切换次数={det['toggles']}, URR用量下降: {prev:.2f}->{cur_vol:.2f}, 窗口={window_seconds}s, 时间={now.isoformat()}Z")
                        last_print[supi] = now
                prev_usage[supi] = cur_vol
            time.sleep(poll_seconds)

if __name__ == "__main__":
    url = os.environ.get("MONGODB_URL", "mongodb://127.0.0.1:27017")
    db = os.environ.get("NWDAF_DB", "nwdaf")
    win = int(os.environ.get("PP_WINDOW_SEC", "120"))
    tog = int(os.environ.get("PP_MIN_TOGGLES", "4"))
    drop = float(os.environ.get("PP_USAGE_DROP_RATIO", "0.3"))
    poll = int(os.environ.get("PP_POLL_SEC", "5"))
    suppress = int(os.environ.get("PP_SUPPRESS_SEC", "30"))
    print(f"启动实时乒乓切换检测: window={win}s, min_toggles={tog}, drop_ratio={drop}, poll={poll}s, suppress={suppress}s")
    run(url, db, win, tog, drop, poll, suppress)