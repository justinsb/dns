#!/bin/sh

set -e
set -x

cat /etc/coredns/Corefile

# exec so we honor signals
exec /node-cache -localip 10.0.16.10 -conf /etc/coredns/Corefile


cat nodelocaldns.yaml.base  | sed s/__PILLAR__LOCAL__DNS__/10.0.16.10/g


# We're using env vars
# TODO: good idea?
KUBE_DNS_IP=${KUBE_DNS_PORT_53_UDP_ADDR}


