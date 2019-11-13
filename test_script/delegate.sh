#!/bin/bash

ROOT=$(dirname $0)

FROM=$1
NODENUM=$2
AMOUNT=$3

AMO1=1000000000000000000

fail() {
	echo "test failed"
	echo $1
	exit -1
}

$ROOT/qb.sh "$NODENUM"
$ROOT/qs.sh "$NODENUM"
$ROOT/qd.sh "$NODENUM"

. testaddr.sh

for ((i=FROM; i<=NODENUM; i++))
do
    addr=tval$i
    out=$(docker exec -it val$i tendermint show_validator | python -c "import sys, json; print json.load(sys.stdin)['value']")
	if [ $? -ne 0 ]; then fail $out; fi

	echo "delegate from tdel$i to tval$i: $(bc <<< "$AMOUNT / $AMO1") AMO"
	out=$($CLI tx delegate $CLIOPT --user tdel$i "${!addr}" "$AMOUNT")
	h=$(echo $out | python -c "import sys, json; print json.load(sys.stdin)['height']")
	if [ -z "$h" -o "$h" == "0" ]; then fail $out; fi
done

$ROOT/qb.sh "$NODENUM"
$ROOT/qs.sh "$NODENUM"
$ROOT/qd.sh "$NODENUM"
