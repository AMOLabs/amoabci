if [ ! -f /tendermint/config/config.toml -a ! -z "$MONIKER" -a ! -z "$PEERS" ]; then
	echo "set moniker = $MONIKER"
	echo "set peers = $PEERS"

	iface=$(route -n | grep '^0.0.0.0' | awk '{print $8}')
	extaddr=$(ip -f inet a show dev $iface | grep '\<inet\>' | head -1 | awk '{print $2}' | awk -F'/' '{print $1}')
	echo "set extaddr = $extaddr"

	cp config.toml.in config.toml
	sed -e s/@moniker@/$MONIKER/ -i.tmp config.toml
	sed -e s/@peers@/$PEERS/ -i.tmp config.toml
	sed -e s/@external@/tcp:\\/\\/$extaddr:26656/ -i.tmp config.toml

	mkdir -p /tendermint/config/
	mv -f config.toml /tendermint/config/
fi

/usr/bin/tendermint init

/usr/bin/amod run &
/usr/bin/tendermint node
