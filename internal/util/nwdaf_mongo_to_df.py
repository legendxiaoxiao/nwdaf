import os
import pandas as pd
from pymongo import MongoClient

def _fetch_df(mongo_url, db_name, coll_name, query=None, projection=None):
    with MongoClient(mongo_url) as client:
        coll = client[db_name][coll_name]
        docs = list(coll.find(query or {}, projection))
    df = pd.json_normalize(docs, sep="_")
    if "_id" in df.columns:
        df["_id"] = df["_id"].astype(str)
    return df

def load_amf_location_reports_df(mongo_url="mongodb://127.0.0.1:27017", db_name="nwdaf", supi=None):
    q = {}
    if supi:
        q["supi"] = supi
    return _fetch_df(mongo_url, db_name, "nwdaf.amf.locationReport", q)

def load_smf_events_df(mongo_url="mongodb://127.0.0.1:27017", db_name="nwdaf", supi=None, event_type=None):
    q = {}
    if supi:
        q["supi"] = supi
    if event_type:
        q["eventType"] = event_type
    return _fetch_df(mongo_url, db_name, "nwdaf.smf.events", q)

if __name__ == "__main__":
    url = os.environ.get("MONGODB_URL", "mongodb://127.0.0.1:27017")
    db = os.environ.get("NWDAF_DB", "nwdaf")
    amf_df = load_amf_location_reports_df(url, db)
    smf_df = load_smf_events_df(url, db)
    print("amf_rows", len(amf_df))
    print("smf_rows", len(smf_df))
    if not amf_df.empty:
        print(amf_df.head(3).to_string(index=False))
    if not smf_df.empty:
        print(smf_df.head(3).to_string(index=False))