#!/bin/sh
#TODO: this file is used for release, should not place it here
set -e

usage() {
    cat <<EOF
Generate certificate suitable for use with an admission controller service.
This script uses k8s' CertificateSigningRequest API to generate a
certificate signed by k8s CA suitable for use with webhook
services. This requires permissions to create and approve CSR. See
https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster for
detailed explanation and additional instructions.
The server key/cert k8s CA cert are stored in a k8s secret.
usage: ${0} [OPTIONS]
The following flags are required.
       --service          Service name of webhook.
       --namespace        Namespace where webhook service and secret reside.
       --secret           Secret name for CA certificate and server certificate/key pair.
EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case ${1} in
        --service)
            SERVICE="$2"
            shift
            ;;
        --secret)
            SECRET="$2"
            shift
            ;;
        --namespace)
            NAMESPACE="$2"
            shift
            ;;
        *)
            usage
            ;;
    esac
    shift
done

if [[ -z ${SERVICE} ]]; then
    echo "'--service' must be specified"
    exit 1
fi

if [[ -z ${SECRET} ]]; then
    echo "'--secret' must be specified"
    exit 1
fi

[[ -z ${NAMESPACE} ]] && NAMESPACE=default

if [[ ! -x "$(command -v openssl)" ]]; then
    echo "openssl not found"
    exit 1
fi

CERTDIR=/tmp

function createCerts() {
  echo "creating certs in dir ${CERTDIR} "

  cat <<EOF > ${CERTDIR}/csr.conf
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
EOF

  openssl genrsa -out ${CERTDIR}/ca.key 2048
  openssl req -x509 -new -nodes -key ${CERTDIR}/ca.key -subj "/CN=${SERVICE}.${NAMESPACE}.svc" -days 3650 -out ${CERTDIR}/ca.crt

  openssl genrsa -out ${CERTDIR}/server.key 2048
  openssl req -new -key ${CERTDIR}/server.key -subj "/CN=${SERVICE}.${NAMESPACE}.svc"  -out ${CERTDIR}/server.csr -config ${CERTDIR}/csr.conf

  openssl x509 -req -in  ${CERTDIR}/server.csr -CA  ${CERTDIR}/ca.crt -CAkey  ${CERTDIR}/ca.key \
  -CAcreateserial -out  ${CERTDIR}/server.crt \
  -extensions v3_req -extfile  ${CERTDIR}/csr.conf -days 3650
}

function createSecret() {
  # create the secret with CA cert and server cert/key
  kubectl create secret generic ${SECRET} \
      --from-file=tls.key=${CERTDIR}/server.key \
      --from-file=tls.crt=${CERTDIR}/server.crt \
      --from-file=ca.crt=${CERTDIR}/ca.crt \
      -n ${NAMESPACE}
}

createCerts

createSecret
