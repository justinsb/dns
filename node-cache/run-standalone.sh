#!/bin/bash

set -ex


# Create the underlying uncached service
kubectl apply -f cluster-dns.yaml

# We will intercept the kube-dns service
LOCAL_DNS=`kubectl get services -n kube-system kube-dns -o=jsonpath={.spec.clusterIP}`

# And we will forward misses to the uncached service we created avod
UPSTREAM_DNS=`kubectl get services -n kube-system cluster-dns -o=jsonpath={.spec.clusterIP}`

# Assume the cluster DNS domain was not changed
DNS_DOMAIN=cluster.local


# TODO: sync nodelocaldns.yaml.base as we change it in k/k
cat nodelocaldns.yaml.base \
  | sed -e s/addonmanager.kubernetes.io/#addonmanager.kubernetes.io/g \
  | sed -e s@kubernetes.io/cluster-service@#kubernetes.io/cluster-service@g \
  | sed -e s@beta.kubernetes.io/nodelocaldns-ready@#beta.kubernetes.io/nodelocaldns-ready@g \
  | sed -e 's@k8s-app: kube-dns@k8s-app: nodelocaldns@g' \
  | sed s/__PILLAR__LOCAL__DNS__/${LOCAL_DNS}/g \
  | sed s/__PILLAR__DNS__SERVER__/${UPSTREAM_DNS}/g \
  | sed -e s/__PILLAR__DNS__DOMAIN__/${DNS_DOMAIN}/g \
  | kubectl apply -f -
