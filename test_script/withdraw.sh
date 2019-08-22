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

. $ROOT/get_key.sh

for ((i=FROM; i<=NODENUM; i++))
do
    echo "retract del$i: $(bc <<< "$AMOUNT / $AMO1") AMO"
	out=$($CLIOPT tx retract $OPT --user tdel$i "$AMOUNT")
	h=$(echo $out | python -c "import sys, json; print json.load(sys.stdin)['height']")
	if [ -z "$h" -o "$h" == "0" ]; then fail $out; fi
done

for ((i=FROM; i<=NODENUM; i++))
do
    echo "withdraw val$i: $(bc <<< "$AMOUNT / $AMO1") AMO"

    # to prevent crash when no stake
    if [ "$i" -eq "$NODENUM" ]; then
		out=$($CLIOPT tx withdraw $OPT --user tval$i "$AMO1")
		h=$(echo $out | python -c "import sys, json; print json.load(sys.stdin)['height']")
		if [ -z "$h" -o "$h" == "0" ]; then fail $out; fi
    else
		out=$($CLIOPT tx withdraw $OPT --user tval$i "$AMOUNT")
		h=$(echo $out | python -c "import sys, json; print json.load(sys.stdin)['height']")
		if [ -z "$h" -o "$h" == "0" ]; then fail $out; fi
    fi
done

$ROOT/qb.sh "$NODENUM"
$ROOT/qs.sh "$NODENUM"
$ROOT/qd.sh "$NODENUM"

