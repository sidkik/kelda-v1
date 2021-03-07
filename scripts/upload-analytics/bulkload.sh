#!/bin/sh

INPUT="${1:-combined-analytics.csv}"

csvgrep -c 1 -m user-study -i "$INPUT" |
    csvsort -c 2 |
    psql -h customer-analytics.cnqlm169uqkq.us-west-2.rds.amazonaws.com \
	 -U analytics \
	 customer_analytics \
	 -c "COPY analytics (customer, time, namespace, event, additional) FROM STDIN DELIMITER ',' CSV HEADER;"
