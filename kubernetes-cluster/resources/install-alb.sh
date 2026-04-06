#!/bin/bash

curl -Lo v2_14_1_full.yaml https://github.com/kubernetes-sigs/aws-load-balancer-controller/releases/download/v2.14.1/v2_14_1_full.yaml

# remove service-account, which we already created in create-alb-roles.sh script
sed -i.bak -e '764,772d' ./v2_14_1_full.yaml
sed -i.bak -e 's|your-cluster-name|socket-tuner|' ./v2_14_1_full.yaml

kubectl apply -f v2_14_1_full.yaml

curl -Lo v2.14.1_ingclass.yaml https://github.com/kubernetes-sigs/aws-load-balancer-controller/releases/download/v2.14.1/v2_14_1_ingclass.yaml
kubectl apply -f v2.14.1_ingclass.yaml
