#!/bin/bash

DATAROOT=$1
NODENUM=$2

# set basic environments
cp -f docker-compose.yml.in docker-compose.yml

sed -e s#__dataroot__#$DATAROOT# -i.tmp docker-compose.yml

mkdir -p $DATAROOT/seed/amo/
mkdir -p $DATAROOT/seed/tendermint/config/
mkdir -p $DATAROOT/seed/tendermint/data/

for ((i=1; i<=NODENUM; i++))
do
    mkdir -p $DATAROOT/val$i/amo/
    mkdir -p $DATAROOT/val$i/tendermint/config/
    mkdir -p $DATAROOT/val$i/tendermint/data/
done

