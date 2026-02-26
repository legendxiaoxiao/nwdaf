import os
import json
from datetime import datetime, timedelta, time as dtime
import streamlit as st
import requests

try:
    from pymongo import MongoClient
except Exception:
    MongoClient = None
try:
    from bson.objectid import ObjectId
except Exception:
    ObjectId = None

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

def _default_since():
    return datetime.utcnow() - timedelta(hours=1)

def _to_rfc3339(dt):
    return dt.replace(microsecond=0).isoformat() + "Z"

def mongo_amf(client, db, supi, since, limit):
    coll = client[db]["nwdaf.amf.locationReport"]
    if supi:
        q = {"supi": supi}
        docs = list(coll.find(q).sort("_id", -1).limit(limit))
        return list(reversed(docs))
    q = {}
    if ObjectId is not None:
        q["_id"] = {"$gt": ObjectId.from_datetime(since)}
    docs = list(coll.find(q).sort("_id", 1))
    if len(docs) > limit:
        docs = docs[-limit:]
    return docs

def mongo_smf_events(client, db, supi, since, limit):
    coll = client[db]["nwdaf.smf.events"]
    q = {"timestamp": {"$gt": since}}
    if supi:
        q["supi"] = supi
    docs = list(coll.find(q).sort("timestamp", 1))
    if len(docs) > limit:
        docs = docs[-limit:]
    return docs

def mongo_smf_usage(client, db, supi, since, limit):
    coll = client[db]["nwdaf.smf.usage"]
    q = {"timestamp": {"$gt": since}}
    if supi:
        q["supi"] = supi
    docs = list(coll.find(q).sort("timestamp", 1))
    if len(docs) > limit:
        docs = docs[-limit:]
    return docs

def mongo_anomaly(client, db, since, limit):
    coll = client[db]["nwdaf.analytics.anomaly"]
    q = {"timestamp": {"$gt": since}}
    docs = list(coll.find(q).sort("timestamp", 1))
    if len(docs) > limit:
        docs = docs[-limit:]
    return docs

def shape_anomaly(docs):
    rows = []
    for d in docs:
        f = d.get("features") or {}
        health = d.get("health")
        if health is None:
            s = float(d.get("score") or 0.0)
            health = max(0.0, 100.0 - s)
        rows.append({
            "timestamp": d.get("timestamp") or "",
            "score": d.get("score") or 0,
            "health": health,
            "status": d.get("status") or "",
            "reg_request_count": f.get("reg_request_count") or 0,
            "total_uplink_volume": f.get("total_uplink_volume") or 0,
            "total_downlink_volume": f.get("total_downlink_volume") or 0,
            "handover_count": f.get("handover_count") or 0,
        })
    return rows

def api_get(base, path, params):
    u = base.rstrip("/") + path
    r = requests.get(u, params=params, timeout=8)
    r.raise_for_status()
    return r.json()

def shape_amf(docs):
    rows = []
    for d in docs:
        loc = d.get("location") or {}
        nr = (loc.get("nrLocation") or {})
        tai = nr.get("tai") or {}
        ncgi = nr.get("ncgi") or {}
        mcc = (tai.get("plmnId") or {}).get("mcc") or ""
        mnc = (tai.get("plmnId") or {}).get("mnc") or ""
        rows.append({
            "supi": d.get("supi") or "",
            "tac": tai.get("tac") or d.get("tac") or "",
            "nrCellId": ncgi.get("nrCellId") or d.get("nrCellId") or "",
            "plmn": f"{mcc}-{mnc}" if mcc or mnc else "",
            "raw": json.dumps(d, ensure_ascii=False),
        })
    return rows

def shape_smf_events(docs):
    rows = []
    for d in docs:
        state = d.get("pduSessionState") or (d.get("eventDetails") or {}).get("pduSessionState") or ""
        qfi = d.get("qfiList") or (d.get("eventDetails") or {}).get("qfiList") or []
        if isinstance(qfi, list):
            qfi = ",".join(str(int(x)) if isinstance(x, (int, float)) else str(x) for x in qfi)
        rows.append({
            "timestamp": d.get("timestamp") or "",
            "eventType": d.get("eventType") or "",
            "supi": d.get("supi") or "",
            "pduSessionId": d.get("pduSessionId") or "",
            "pduState": state,
            "qfiList": qfi,
            "raw": json.dumps(d, ensure_ascii=False),
        })
    return rows

def shape_smf_usage(docs):
    rows = []
    for d in docs:
        rows.append({
            "timestamp": d.get("timestamp") or "",
            "supi": d.get("supi") or "",
            "pduSessionId": d.get("pduSessionId") or "",
            "usageSum": f"{_sum_numeric(d.get('usageData')):.2f}",
            "raw": json.dumps(d, ensure_ascii=False),
        })
    return rows

st.set_page_config(page_title="NWDAF 前端", layout="wide")

src = st.sidebar.radio("数据源", ["MongoDB", "NWDAF API"], index=0)
supi = st.sidebar.text_input("SUPI", "")
ds = _default_since().date()
ts = (datetime.utcnow() - timedelta(hours=1)).time().replace(second=0, microsecond=0)
sd = st.sidebar.date_input("起始日期(UTC)", ds)
stt = st.sidebar.time_input("起始时间(UTC)", ts)
lim = st.sidebar.number_input("Limit", min_value=1, max_value=2000, value=200, step=50)

if src == "MongoDB":
    murl = st.sidebar.text_input("MongoDB URL", os.getenv("MONGODB_URL", "mongodb://127.0.0.1:27017"))
    mdb = st.sidebar.text_input("数据库名", os.getenv("NWDAF_DB", "nwdaf"))
else:
    base = st.sidebar.text_input("NWDAF API Base", os.getenv("NWDAF_API_BASE", "http://127.0.0.1:8001"))

since_dt = datetime.combine(sd, dtime(stt.hour, stt.minute))
st.sidebar.button("刷新")
auto = st.sidebar.checkbox("自动刷新", value=False)
interval = st.sidebar.number_input("刷新间隔(秒)", min_value=5, max_value=300, value=30, step=5)
if auto:
    st.components.v1.html(f"<script>setTimeout(function(){{window.location.reload();}}, {int(interval)*1000});</script>", height=0, width=0)

tab0, tab1, tab2, tab3 = st.tabs(["健康评分", "AMF 位置报告", "SMF 事件", "SMF 用量"])

if src == "MongoDB":
    if MongoClient is None or ObjectId is None:
        st.error("缺少 PyMongo 或 BSON 依赖")
    else:
        try:
            with MongoClient(murl) as cli:
                an_docs = mongo_anomaly(cli, mdb, since_dt, lim)
                amf_docs = mongo_amf(cli, mdb, supi, since_dt, lim)
                ev_docs = mongo_smf_events(cli, mdb, supi, since_dt, lim)
                us_docs = mongo_smf_usage(cli, mdb, supi, since_dt, lim)
                with tab0:
                    st.caption(f"健康评分 共 {len(an_docs)} 条")
                    if an_docs:
                        latest = an_docs[-1]
                        st.metric("异常指数", f"{float(latest.get('score', 0)):.2f}")
                        h = latest.get('health')
                        if h is None:
                            h = max(0.0, 100.0 - float(latest.get('score', 0)))
                        st.metric("健康评分", f"{float(h):.2f}")
                        st.text(f"状态: {latest.get('status', '')}")
                    st.table(shape_anomaly(an_docs))
                with tab1:
                    st.caption(f"AMF 报告 共 {len(amf_docs)} 条")
                    st.table(shape_amf(amf_docs))
                with tab2:
                    st.caption(f"SMF 事件 共 {len(ev_docs)} 条")
                    st.table(shape_smf_events(ev_docs))
                with tab3:
                    st.caption(f"SMF 用量 共 {len(us_docs)} 条")
                    st.table(shape_smf_usage(us_docs))
        except Exception as e:
            st.error(str(e))
else:
    try:
        params = {}
        if supi:
            params["supi"] = supi
        params["since"] = _to_rfc3339(since_dt)
        params["limit"] = str(lim)
        amf_docs = api_get(base, "/nnwdaf-events/v1/amf-reports", params)
        ev_docs = api_get(base, "/nnwdaf-events/v1/smf-events", params)
        us_docs = api_get(base, "/nnwdaf-events/v1/smf-usage", params)
        if not isinstance(amf_docs, list): amf_docs = []
        if not isinstance(ev_docs, list): ev_docs = []
        if not isinstance(us_docs, list): us_docs = []
        with tab0:
            st.info("NWDAF API 暂未提供健康评分接口，请选择 MongoDB 数据源")
        with tab1:
            st.caption(f"AMF 报告 共 {len(amf_docs)} 条")
            st.table(shape_amf(amf_docs))
        with tab2:
            st.caption(f"SMF 事件 共 {len(ev_docs)} 条")
            st.table(shape_smf_events(ev_docs))
        with tab3:
            st.caption(f"SMF 用量 共 {len(us_docs)} 条")
            st.table(shape_smf_usage(us_docs))
    except Exception as e:
        st.error(str(e))