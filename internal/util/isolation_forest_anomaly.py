import os
import time
from datetime import datetime, timedelta
from typing import Dict, List, Tuple

import numpy as np
from pymongo import MongoClient
from bson import ObjectId
from sklearn.ensemble import IsolationForest


def _env(key: str, default: str) -> str:
    v = os.environ.get(key)
    return v if v else default


def _floor_minute(dt: datetime) -> datetime:
    return dt.replace(second=0, microsecond=0)


def _extract_up_down(usage: dict) -> Tuple[float, float]:
    if not isinstance(usage, dict):
        return 0.0, 0.0
    up_keys = [
        "uplinkVolume",
        "ulBytes",
        "bytesUp",
        "uplink",
        "UL",
        "up",
        "tx",
        "uplink_bytes",
        "ULData",
        "ULDataVolume",
    ]
    down_keys = [
        "downlinkVolume",
        "dlBytes",
        "bytesDown",
        "downlink",
        "DL",
        "down",
        "rx",
        "downlink_bytes",
        "DLData",
        "DLDataVolume",
    ]
    def _sum_keys(d: dict, keys: List[str]) -> float:
        s = 0.0
        for k in keys:
            v = d.get(k)
            if isinstance(v, (int, float)):
                s += float(v)
        for k, v in d.items():
            if isinstance(v, dict):
                s += _sum_keys(v, keys)
        return s
    up = _sum_keys(usage, up_keys)
    down = _sum_keys(usage, down_keys)
    return up, down


def _handover_count(coll, start: datetime, end: datetime) -> int:
    q = {"_id": {"$gt": ObjectId.from_datetime(start), "$lte": ObjectId.from_datetime(end)}}
    cur = coll.find(q, {"supi": 1, "nrCellId": 1}).sort([("supi", 1), ("_id", 1)])
    last: Dict[str, str] = {}
    cnt = 0
    for doc in cur:
        supi = doc.get("supi")
        cell = doc.get("nrCellId")
        if not supi or not cell:
            continue
        prev = last.get(supi)
        if prev is not None and cell != prev:
            cnt += 1
        last[supi] = cell
    return cnt


def _window_features(cli: MongoClient, dbname: str, start: datetime, end: datetime) -> Dict[str, float]:
    db = cli[dbname]
    reg_coll = db["nwdaf.amf.reg_request_count"]
    usage_coll = db["nwdaf.smf.usage"]
    loc_coll = db["nwdaf.amf.locationReport"]

    rq = {"timestamp": {"$gt": start, "$lte": end}}
    reg_count = 0.0
    for d in reg_coll.find(rq, {"count": 1, "value": 1, "timestamp": 1}):
        c = d.get("count")
        if isinstance(c, (int, float)):
            reg_count += float(c)
        else:
            v = d.get("value")
            if isinstance(v, (int, float)):
                reg_count += float(v)

    uq = {"timestamp": {"$gt": start, "$lte": end}}
    up_sum = 0.0
    down_sum = 0.0
    for d in usage_coll.find(uq, {"usageData": 1, "timestamp": 1}):
        u, dl = _extract_up_down(d.get("usageData"))
        up_sum += u
        down_sum += dl

    ho_count = _handover_count(loc_coll, start, end)

    return {
        "reg_request_count": reg_count,
        "total_uplink_volume": up_sum,
        "total_downlink_volume": down_sum,
        "handover_count": float(ho_count),
    }


def _build_training(cli: MongoClient, dbname: str, end: datetime, minutes: int) -> List[List[float]]:
    X: List[List[float]] = []
    for i in range(minutes):
        s = end - timedelta(minutes=i + 1)
        e = end - timedelta(minutes=i)
        f = _window_features(cli, dbname, s, e)
        x = [
            f["reg_request_count"],
            f["total_uplink_volume"],
            f["total_downlink_volume"],
            f["handover_count"],
        ]
        X.append(x)
    X.reverse()
    return X


def _parse_rfc3339(s: str) -> datetime:
    try:
        return datetime.strptime(s.replace('Z',''), '%Y-%m-%dT%H:%M:%S')
    except Exception:
        return None


def _build_baseline(cli: MongoClient, dbname: str, end: datetime, base_minutes: int, since_str: str) -> Tuple[List[List[float]], Dict]:
    use_all = os.environ.get("NWDAF_ANOMALY_BASELINE_USE_ALL", "1") == "1"
    max_minutes = int(_env("NWDAF_ANOMALY_BASELINE_MAX_MINUTES", "10080"))
    minutes = base_minutes if base_minutes and base_minutes > 0 else 1440

    earliest = None
    try:
        rc = cli[dbname]["nwdaf.amf.reg_request_count"]
        r = list(rc.find({}, {"timestamp": 1}).sort("timestamp", 1).limit(1))
        if r:
            earliest = r[0].get("timestamp")
        uc = cli[dbname]["nwdaf.smf.usage"]
        u = list(uc.find({}, {"timestamp": 1}).sort("timestamp", 1).limit(1))
        if u:
            tsu = u[0].get("timestamp")
            earliest = tsu if earliest is None else min(earliest, tsu)
        lc = cli[dbname]["nwdaf.amf.locationReport"]
        l = list(lc.find({}, {"_id": 1}).sort("_id", 1).limit(1))
        if l:
            oid = l[0].get("_id")
            if isinstance(oid, ObjectId):
                et = oid.generation_time.replace(tzinfo=None)
                earliest = et if earliest is None else min(earliest, et)
    except Exception:
        pass

    if since_str:
        dt = _parse_rfc3339(since_str)
        if dt and (earliest is None or dt > earliest) and dt < end:
            earliest = dt

    if earliest and earliest < end:
        delta = int((end - earliest).total_seconds() // 60)
        if use_all:
            minutes = max(1, min(delta, max_minutes))
        else:
            minutes = min(minutes, delta)

    Xb = _build_training(cli, dbname, end, minutes)
    arr = np.array(Xb, dtype=float)
    stats = {}
    if arr.size > 0:
        stats = {
            'sampleSize': int(arr.shape[0]),
            'median': np.median(arr, axis=0).tolist(),
            'q25': np.quantile(arr, 0.25, axis=0).tolist(),
            'q75': np.quantile(arr, 0.75, axis=0).tolist(),
            'q90': np.quantile(arr, 0.90, axis=0).tolist(),
            'q95': np.quantile(arr, 0.95, axis=0).tolist(),
            'q99': np.quantile(arr, 0.99, axis=0).tolist(),
        }
    return Xb, stats


def _classify_alerts(f: Dict[str, float], stats: Dict, score: float, alert_threshold: float, storm_q: float) -> List[Dict]:
    alerts = []
    qn = None
    if stats:
        if storm_q >= 0.99 and 'q99' in stats:
            qn = stats['q99']
        elif 'q95' in stats:
            qn = stats['q95']
    if qn is not None:
        if f.get('reg_request_count', 0.0) > float(qn[0]) and score >= alert_threshold:
            alerts.append({'type': 'SIGNALING_STORM', 'reason': 'reg_request_count high vs baseline', 'threshold': qn[0]})
        if f.get('handover_count', 0.0) > float(qn[3]) and score >= alert_threshold:
            alerts.append({'type': 'HANDOVER_ABNORMAL', 'reason': 'handover_count high vs baseline', 'threshold': qn[3]})
    return alerts


def _transform_features(f: Dict[str, float], stats: Dict) -> List[float]:
    med = stats.get('median') or [0,0,0,0]
    q25 = stats.get('q25') or med
    q75 = stats.get('q75') or med
    iqr = [max(q75[i]-q25[i], 1e-9) for i in range(4)]
    v1 = float(f.get('reg_request_count', 0.0))
    v2 = float(f.get('total_uplink_volume', 0.0))
    v3 = float(f.get('total_downlink_volume', 0.0))
    v4 = float(f.get('handover_count', 0.0))
    v2 = np.log1p(v2)
    v3 = np.log1p(v3)
    return [
        (v1 - med[0]) / iqr[0],
        (v2 - np.log1p(med[1])) / max(np.log1p(q75[1]) - np.log1p(q25[1]), 1e-9),
        (v3 - np.log1p(med[2])) / max(np.log1p(q75[2]) - np.log1p(q25[2]), 1e-9),
        (v4 - med[3]) / iqr[3],
    ]


def _within_baseline(f: Dict[str, float], stats: Dict, q: float) -> bool:
    key_order = ['reg_request_count','total_uplink_volume','total_downlink_volume','handover_count']
    qn = stats.get('q95') if abs(q - 0.95) < 1e-6 else stats.get('q99') if abs(q - 0.99) < 1e-6 else stats.get('q90')
    if not qn:
        return False
    for i,k in enumerate(key_order):
        if float(f.get(k,0.0)) > float(qn[i]):
            return False
    return True


def _score_isoforest(X_train: List[List[float]], x_cur: List[float], contamination: float) -> float:
    if len(X_train) < 10:
        return 0.0
    iso = IsolationForest(random_state=42, contamination=contamination, n_estimators=100)
    iso.fit(X_train)
    s_train = iso.score_samples(X_train)
    s_cur = iso.score_samples([x_cur])[0]
    pct = float(np.mean(s_train <= s_cur))
    idx = (1.0 - pct) * 100.0
    if idx < 0.0:
        idx = 0.0
    if idx > 100.0:
        idx = 100.0
    return float(idx)


def run_once(cli: MongoClient, dbname: str, train_minutes: int, contamination: float) -> Dict:
    now = datetime.utcnow()
    end = _floor_minute(now)
    start = end - timedelta(minutes=1)
    f = _window_features(cli, dbname, start, end)
    x_cur = [
        f["reg_request_count"],
        f["total_uplink_volume"],
        f["total_downlink_volume"],
        f["handover_count"],
    ]
    base_minutes = int(_env("NWDAF_ANOMALY_BASELINE_MINUTES", "1440"))
    base_since = os.environ.get("NWDAF_ANOMALY_BASELINE_SINCE", "")
    alert_threshold = float(_env("NWDAF_ANOMALY_ALERT_THRESHOLD", "80"))
    storm_q = float(_env("NWDAF_ANOMALY_STORM_QUANTILE", "0.99"))

    X_base, stats = _build_baseline(cli, dbname, end, base_minutes, base_since)
    X_train_raw = X_base if len(X_base) >= 10 else _build_training(cli, dbname, end, train_minutes)
    X_train = [
        _transform_features({
            'reg_request_count': xr[0],
            'total_uplink_volume': xr[1],
            'total_downlink_volume': xr[2],
            'handover_count': xr[3]
        }, stats) for xr in X_train_raw
    ]
    x_cur_t = _transform_features(f, stats)
    score = _score_isoforest(X_train, x_cur_t, contamination)
    clamp_q = float(_env('NWDAF_ANOMALY_CLAMP_Q', '0.95'))
    clamp_max = float(_env('NWDAF_ANOMALY_CLAMP_MAX', '20'))
    if _within_baseline(f, stats, clamp_q):
        score = min(score, clamp_max)
    health = float(max(0.0, 100.0 - score))
    alerts = _classify_alerts(f, stats, score, alert_threshold, storm_q)
    status = "OK"
    if health < 60.0:
        status = "ALERT"
    elif health < 80.0:
        status = "WARN"

    doc = {
        "timestamp": end,
        "windowStart": start,
        "windowEnd": end,
        "features": f,
        "score": score,
        "health": health,
        "status": status,
        "alerts": alerts,
        "featureOrder": ["reg_request_count", "total_uplink_volume", "total_downlink_volume", "handover_count"],
        "baselineStats": stats,
        "model": {"type": "IsolationForest", "trainMinutes": train_minutes, "contamination": contamination,
                   "baselineMinutes": base_minutes, "stormQuantile": storm_q, "alertThreshold": alert_threshold},
    }
    cli[dbname]["nwdaf.analytics.anomaly"].update_one({"timestamp": end}, {"$set": doc}, upsert=True)
    return doc


def main():
    url = _env("MONGODB_URL", "mongodb://127.0.0.1:27017")
    dbname = _env("NWDAF_DB", "nwdaf")
    train_minutes = int(_env("NWDAF_ANOMALY_TRAIN_MINUTES", "60"))
    contamination = float(_env("NWDAF_ANOMALY_CONTAMINATION", "0.05"))
    loop = os.environ.get("NWDAF_ANOMALY_LOOP", "0") == "1"
    with MongoClient(url) as cli:
        if loop:
            while True:
                doc = run_once(cli, dbname, train_minutes, contamination)
                now = datetime.utcnow()
                next_tick = _floor_minute(now) + timedelta(minutes=1)
                sleep_sec = max(0.5, (next_tick - now).total_seconds())
                time.sleep(sleep_sec)
        else:
            doc = run_once(cli, dbname, train_minutes, contamination)
            print({"timestamp": doc["timestamp"].isoformat(), "score": doc["score"], "features": doc["features"]})


if __name__ == "__main__":
    main()