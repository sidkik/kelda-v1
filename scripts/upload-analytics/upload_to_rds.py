#!/usr/bin/env python3

import configparser
import psycopg2
import csv
import time
from operator import itemgetter

CREDS_FILE = "creds.conf"
ANALYTICS_FILE = "combined-analytics.csv"
INSERT_QUERY = 'INSERT INTO analytics (time, customer, namespace, event, additional) VALUES (%s, %s, %s, %s, %s);'
LAST_INDEX_QUERY = 'SELECT MAX(id) FROM analytics;'

cfg = configparser.ConfigParser()
cfg.read(CREDS_FILE)

def get_next_index(cursor):
    cursor.execute(LAST_INDEX_QUERY)
    result = cursor.fetchone()[0] or -1
    return int(result) + 1

def read_input(source=ANALYTICS_FILE):
    with open(source, 'r') as src:
        reader = csv.DictReader(src)
        for row in reader:
            # skip user-study lines since they interfere with the incremental load logic
            if row['customer'].startswith('user-study'):
               continue

            yield row

def sort_input(read_function):
    return sorted(read_function, key=itemgetter('time'))

def upload_to_rds(cursor, time, customer, namespace, event, additional):
    cursor.execute(INSERT_QUERY, (time, customer, namespace, event, additional))
    pass

def main():

    conn = psycopg2.connect(
        user=cfg['analytics']['username'],
        host=cfg['analytics']['hostname'],
        password=cfg['analytics']['password'],
        database=cfg['analytics']['database'],
    )

    cursor = conn.cursor()

    try:
        start_idx = get_next_index(cursor)
        print("startng from {}".format(start_idx))

        # spin through the lines we've already uploaded
        enumerated_input = enumerate(sort_input(read_input()))
        for i in range(start_idx):
            next(enumerated_input)

        # upload what are theoretically new lines
        for idx, v in enumerated_input:
            start = time.time()
            upload_to_rds(
                cursor,
                v['time'],
                v['customer'],
                v['namespace'],
                v['event'],
                v['additional']
            )
            end = time.time()

            print("time to process row {}: {} {}".format(idx, end - start, v['time']))

        db.commit()

    except Exception as e:
        print(e)

    finally:
        cursor.close()
        conn.close()

if __name__=='__main__':
    main()
