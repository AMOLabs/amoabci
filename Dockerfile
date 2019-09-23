# vim: set expandtab:

#### builder image

FROM golang:1.12-alpine3.9

# tools
RUN apk add bash git make gcc g++

# libs
RUN apk add snappy

RUN mkdir /src
WORKDIR /src

# leveldb
RUN wget https://github.com/google/leveldb/archive/v1.20.tar.gz
RUN tar zxvf v1.20.tar.gz && make -C leveldb-1.20
RUN cp -a leveldb-1.20/include/leveldb /usr/include/
RUN cp -a leveldb-1.20/out-shared/libleveldb.so* /usr/lib/

# tendermint
RUN git clone -b v0.32.3 https://github.com/tendermint/tendermint
RUN make -C tendermint get_tools && make -C tendermint build_c

# amod
RUN mkdir -p amoabci
COPY Makefile go.mod go.sum amoabci/
COPY cmd amoabci/cmd
COPY amo amoabci/amo
COPY crypto amoabci/crypto
RUN make -C amoabci build_c

#### runner image

FROM alpine:3.9

# tools & libs
RUN apk add bash snappy

#COPY tendermint amod /usr/bin/
COPY --from=0 /usr/lib/libleveldb.so* /usr/lib/
COPY --from=0 /usr/lib/libgcc_s.so* /usr/lib/
COPY --from=0 /usr/lib/libstdc++.so* /usr/lib/
COPY --from=0 /src/tendermint/build/tendermint /usr/bin/
COPY --from=0 /src/amoabci/amod /usr/bin/
COPY DOCKER/run_node.sh DOCKER/config/* /

ENV AMOHOME /amo
ENV TMHOME /tendermint
VOLUME [ $AMOHOME ]
VOLUME [ $TMHOME ]

WORKDIR /

EXPOSE 26656 26657

CMD ["/bin/sh", "/run_node.sh"]
