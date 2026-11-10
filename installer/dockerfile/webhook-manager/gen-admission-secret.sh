#!/bin/sh

# Generate or repair Volcano admission webhook certificates.
#
# IMPORTANT:
# This script only manages the admission TLS Secret.
# The caller/Helm deployment workflow must ensure:
#   1. The webhook caBundle trusts the generated CA.
#   2. The admission deployment is restarted after certificate rotation.
#   3. The admission deployment is ready before webhook traffic is enabled.
# This script does not manage caBundle synchronization or deployment rollout.
#
# Existing valid certificates are reused.
# Invalid or expired certificates are automatically regenerated and patched.
#
# Requirements:
#   - POSIX-compatible /bin/sh (e.g. BusyBox ash)
#   - kubectl
#   - OpenSSL with x509 -checkhost and -checkend support (LibreSSL is not supported)
#   - base64, tr, mktemp, awk, grep, rm
#
# Example:
#   ./gen-admission-secret.sh \
#     --service volcano-admission-service \
#     --namespace kube-system \
#     --secret volcano-admission-secret

set -eu

CERT_DAYS=5475
WARN_DAYS=30
WARN_SECONDS=$((WARN_DAYS * 24 * 60 * 60))

SERVICE=
NAMESPACE=
SECRET=
WORK_DIR=
CA_CERT=
TLS_CERT=
TLS_KEY=
SECRET_EXISTS=false
CA_B64=
TLS_CERT_B64=
TLS_KEY_B64=

log() {
    printf '[INFO] %s\n' "$*" >&2
}

warn() {
    printf '[WARN] %s\n' "$*" >&2
}

die() {
    printf '[ERROR] %s\n' "$*" >&2
    exit 1
}

cleanup() {
    if [ -n "${WORK_DIR}" ] && [ -d "${WORK_DIR}" ]; then
        rm -rf "${WORK_DIR}"
    fi
}

trap cleanup 0
trap 'exit 1' HUP INT TERM

usage() {
    cat <<USAGE
Usage:
  $0 --service SERVICE --namespace NAMESPACE --secret SECRET

Options:
  --service       Admission Service name
  --namespace     Kubernetes namespace
  --secret        Kubernetes Secret name
  -h, --help      Show this help
USAGE
}

# Validate a Kubernetes DNS-1123 label.
validate_dns_label() {
    vdl_name=$1
    vdl_field=$2

    case "${vdl_name}" in
        ''|*[!a-z0-9-]*|-*|*-)
            die "Invalid ${vdl_field}: ${vdl_name}"
            ;;
    esac

    if [ "${#vdl_name}" -gt 63 ]; then
        die "${vdl_field} must not exceed 63 characters: ${vdl_name}"
    fi
}

# Validate a Kubernetes DNS subdomain, including per-label and total lengths.
validate_dns_subdomain() {
    vds_name=$1
    vds_field=$2

    case "${vds_name}" in
        ''|.*|*.|*..*|*[!a-z0-9.-]*)
            die "Invalid ${vds_field}: ${vds_name}"
            ;;
    esac

    if [ "${#vds_name}" -gt 253 ]; then
        die "${vds_field} must not exceed 253 characters: ${vds_name}"
    fi

    # awk validates each dot-separated label without relying on shell arrays.
    printf '%s\n' "${vds_name}" | awk -F. '
        {
            for (i = 1; i <= NF; i++) {
                if (length($i) == 0 || length($i) > 63 ||
                    $i ~ /^-/ || $i ~ /-$/) {
                    exit 1
                }
            }
        }
    ' || die "Invalid ${vds_field} DNS label: ${vds_name}"
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --service)
                [ "$#" -ge 2 ] || die "Missing value for --service"
                SERVICE=$2
                shift 2
                ;;
            --namespace)
                [ "$#" -ge 2 ] || die "Missing value for --namespace"
                NAMESPACE=$2
                shift 2
                ;;
            --secret)
                [ "$#" -ge 2 ] || die "Missing value for --secret"
                SECRET=$2
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                die "Unknown argument: $1"
                ;;
        esac
    done

    [ -n "${SERVICE}" ] || die "--service is required"
    [ -n "${NAMESPACE}" ] || die "--namespace is required"
    [ -n "${SECRET}" ] || die "--secret is required"

    validate_dns_label "${SERVICE}" service
    validate_dns_label "${NAMESPACE}" namespace
    validate_dns_subdomain "${SECRET}" secret
}

check_dependencies() {
    for cd_cmd in kubectl openssl base64 tr mktemp awk grep rm; do
        command -v "${cd_cmd}" >/dev/null 2>&1 ||
            die "Required command not found: ${cd_cmd}"
    done

    cd_openssl_version=$(openssl version 2>/dev/null | awk '{print $2}') ||
        die "Failed to execute openssl version"
    cd_openssl_help=$(openssl x509 -help 2>&1 || true)

    printf '%s\n' "${cd_openssl_help}" | grep -q -- '-checkhost' ||
        die "openssl x509 does not support -checkhost"
    printf '%s\n' "${cd_openssl_help}" | grep -q -- '-checkend' ||
        die "openssl x509 does not support -checkend"

    log "Using OpenSSL ${cd_openssl_version}"
}

init_work_dir() {
    umask 077
    WORK_DIR=$(mktemp -d) || die "Failed to create temporary directory"
    CA_CERT=${WORK_DIR}/ca.crt
    TLS_CERT=${WORK_DIR}/tls.crt
    TLS_KEY=${WORK_DIR}/tls.key
}

# The sentinel distinguishes a missing Secret from an existing Secret with
# missing or empty data fields.
fetch_secret() {
    fs_output=
    SECRET_EXISTS=false
    CA_B64=
    TLS_CERT_B64=
    TLS_KEY_B64=

    fs_output=$(kubectl get secret "${SECRET}" \
        -n "${NAMESPACE}" \
        --ignore-not-found \
        -o go-template='{{"__SECRET_EXISTS__\n"}}{{range $k, $v := .data}}{{if eq $k "ca.crt"}}CA={{$v}}{{"\n"}}{{end}}{{if eq $k "tls.crt"}}TLS_CERT={{$v}}{{"\n"}}{{end}}{{if eq $k "tls.key"}}TLS_KEY={{$v}}{{"\n"}}{{end}}{{end}}') ||
        die "Failed to query Secret ${NAMESPACE}/${SECRET}"

    if [ -z "${fs_output}" ]; then
        SECRET_EXISTS=false
        return 0
    fi

    fs_first_line=true
    fs_seen_sentinel=false
    while IFS= read -r fs_line || [ -n "${fs_line}" ]; do
        if [ "${fs_first_line}" = true ]; then
            [ "${fs_line}" = '__SECRET_EXISTS__' ] ||
                die "Unexpected output when querying Secret ${NAMESPACE}/${SECRET}"
            fs_first_line=false
            fs_seen_sentinel=true
            continue
        fi

        case "${fs_line}" in
            CA=*) CA_B64=${fs_line#CA=} ;;
            TLS_CERT=*) TLS_CERT_B64=${fs_line#TLS_CERT=} ;;
            TLS_KEY=*) TLS_KEY_B64=${fs_line#TLS_KEY=} ;;
        esac
    done <<EOF_LINES
${fs_output}
EOF_LINES

    [ "${fs_seen_sentinel}" = true ] ||
        die "Unexpected output when querying Secret ${NAMESPACE}/${SECRET}"
    SECRET_EXISTS=true
}

decode_secret_data() {
    [ -n "${CA_B64}" ] || return 1
    [ -n "${TLS_CERT_B64}" ] || return 1
    [ -n "${TLS_KEY_B64}" ] || return 1

    printf '%s' "${CA_B64}" | base64 -d > "${CA_CERT}" || return 1
    printf '%s' "${TLS_CERT_B64}" | base64 -d > "${TLS_CERT}" || return 1
    printf '%s' "${TLS_KEY_B64}" | base64 -d > "${TLS_KEY}" || return 1

    [ -s "${CA_CERT}" ] || return 1
    [ -s "${TLS_CERT}" ] || return 1
    [ -s "${TLS_KEY}" ] || return 1
    return 0
}

validate_certificates() {
    vc_cert_pub=${WORK_DIR}/cert.pub
    vc_cert_der=${WORK_DIR}/cert.der
    vc_cert_hash=${WORK_DIR}/cert.hash
    vc_key_der=${WORK_DIR}/key.der
    vc_key_hash=${WORK_DIR}/key.hash
    vc_tls_text=${WORK_DIR}/tls-cert.txt

    [ -s "${CA_CERT}" ] || return 1
    [ -s "${TLS_CERT}" ] || return 1
    [ -s "${TLS_KEY}" ] || return 1

    openssl x509 -in "${CA_CERT}" -noout >/dev/null 2>&1 || return 1
    openssl x509 -in "${TLS_CERT}" -noout >/dev/null 2>&1 || return 1
    openssl pkey -in "${TLS_KEY}" -noout >/dev/null 2>&1 || return 1

    # This script expects a self-signed root CA.
    openssl verify -CAfile "${CA_CERT}" "${CA_CERT}" >/dev/null 2>&1 || return 1

    openssl verify -purpose sslserver -CAfile "${CA_CERT}" "${TLS_CERT}" >/dev/null 2>&1 || return 1

    # Require an explicit SAN entry, not a Common Name fallback.
    openssl x509 -in "${TLS_CERT}" -noout -text > "${vc_tls_text}" 2>/dev/null || return 1
    awk -v expected="DNS:${SERVICE}.${NAMESPACE}.svc" '
        /X509v3 Subject Alternative Name:/ { in_san = 1; next }
        in_san && /^[[:space:]]*X509v3 / { in_san = 0 }
        in_san && index($0, expected) > 0 { found = 1 }
        END { exit !found }
    ' "${vc_tls_text}" || return 1
    openssl x509 -in "${TLS_CERT}" -noout -checkhost "${SERVICE}.${NAMESPACE}.svc" >/dev/null 2>&1 || return 1

    openssl x509 -in "${CA_CERT}" -noout -checkend 0 >/dev/null 2>&1 || return 1
    openssl x509 -in "${TLS_CERT}" -noout -checkend 0 >/dev/null 2>&1 || return 1

    # Check each command separately; POSIX sh has no pipefail.
    openssl x509 -in "${TLS_CERT}" -pubkey -noout > "${vc_cert_pub}" || return 1
    openssl pkey -pubin -in "${vc_cert_pub}" -outform DER > "${vc_cert_der}" || return 1
    openssl dgst -sha256 "${vc_cert_der}" > "${vc_cert_hash}" || return 1

    openssl pkey -in "${TLS_KEY}" -pubout -outform DER > "${vc_key_der}" || return 1
    openssl dgst -sha256 "${vc_key_der}" > "${vc_key_hash}" || return 1

    vc_cert_hash_value=$(awk '{print $NF}' "${vc_cert_hash}") || return 1
    vc_key_hash_value=$(awk '{print $NF}' "${vc_key_hash}") || return 1
    [ "${vc_cert_hash_value}" = "${vc_key_hash_value}" ] || return 1

    return 0
}

check_expiration_warning() {
    cew_cert_file=$1
    cew_cert_name=$2

    if ! openssl x509 -in "${cew_cert_file}" -noout -checkend "${WARN_SECONDS}" >/dev/null 2>&1; then
        warn "${cew_cert_name} will expire within ${WARN_DAYS} days; reusing the existing certificate"
    fi
}

generate_certificates() {
    gc_ca_config=${WORK_DIR}/ca.cnf
    gc_tls_ext=${WORK_DIR}/tls.ext
    gc_tls_csr=${WORK_DIR}/tls.csr

    log "Generating new admission certificates (${CERT_DAYS} days)"

    openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "${WORK_DIR}/ca.key"

    cat > "${gc_ca_config}" <<EOF_CA
[req]
prompt = no
distinguished_name = dn
x509_extensions = v3_ca

[dn]
CN = ${SERVICE}.${NAMESPACE}.svc CA

[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
EOF_CA

    openssl req -x509 -new -key "${WORK_DIR}/ca.key" -sha256 \
        -days "${CERT_DAYS}" -config "${gc_ca_config}" \
        -extensions v3_ca -out "${CA_CERT}"

    openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "${TLS_KEY}"
    openssl req -new -key "${TLS_KEY}" \
        -subj "/CN=${SERVICE}.${NAMESPACE}.svc" -out "${gc_tls_csr}"

    cat > "${gc_tls_ext}" <<EOF_TLS
[server_cert]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
DNS.4 = ${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF_TLS

    openssl x509 -req -in "${gc_tls_csr}" \
        -CA "${CA_CERT}" -CAkey "${WORK_DIR}/ca.key" \
        -CAcreateserial -out "${TLS_CERT}" -days "${CERT_DAYS}" \
        -sha256 -extfile "${gc_tls_ext}" -extensions server_cert

    validate_certificates || die "Generated certificates failed validation"
    log "New certificates generated and validated successfully"
}

create_secret() {
    log "Creating Secret ${NAMESPACE}/${SECRET}"
    kubectl create secret generic "${SECRET}" \
        -n "${NAMESPACE}" \
        --type=kubernetes.io/tls \
        --from-file=ca.crt="${CA_CERT}" \
        --from-file=tls.crt="${TLS_CERT}" \
        --from-file=tls.key="${TLS_KEY}"
}

patch_secret() {
    ps_ca_b64=$(base64 < "${CA_CERT}" | tr -d '\n') || die "Failed to encode CA certificate"
    ps_tls_cert_b64=$(base64 < "${TLS_CERT}" | tr -d '\n') || die "Failed to encode TLS certificate"
    ps_tls_key_b64=$(base64 < "${TLS_KEY}" | tr -d '\n') || die "Failed to encode TLS private key"

    ps_patch=$(printf '{"data":{"ca.crt":"%s","tls.crt":"%s","tls.key":"%s"}}' \
        "${ps_ca_b64}" "${ps_tls_cert_b64}" "${ps_tls_key_b64}") ||
        die "Failed to construct Secret patch"

    log "Patching Secret ${NAMESPACE}/${SECRET}"
    kubectl patch secret "${SECRET}" -n "${NAMESPACE}" --type=merge -p "${ps_patch}"
}

verify_written_secret() {
    log "Reading back Secret ${NAMESPACE}/${SECRET} for verification"
    fetch_secret

    [ "${SECRET_EXISTS}" = true ] ||
        die "Secret ${NAMESPACE}/${SECRET} does not exist after writing"
    decode_secret_data ||
        die "Failed to decode Secret ${NAMESPACE}/${SECRET} after writing"
    validate_certificates ||
        die "Secret ${NAMESPACE}/${SECRET} failed certificate validation after writing"

    log "Secret read-back verification succeeded"
}

main() {
    parse_args "$@"
    check_dependencies
    init_work_dir

    log "Checking Secret ${NAMESPACE}/${SECRET}"
    fetch_secret

    if [ "${SECRET_EXISTS}" = false ]; then
        log "Secret does not exist; generating certificates"
        generate_certificates
        create_secret
        verify_written_secret
        log "Admission certificates created successfully"
        warn "Ensure the webhook caBundle trusts the generated CA before enabling webhook traffic."
        return 0
    fi

    log "Secret exists; validating existing certificates"
    if decode_secret_data && validate_certificates; then
        log "Existing certificates are valid; reusing them"
        check_expiration_warning "${CA_CERT}" "CA certificate"
        check_expiration_warning "${TLS_CERT}" "TLS certificate"
        return 0
    fi

    warn "Existing Secret contains invalid, expired, missing, or corrupted certificate data"
    warn "Generating replacement certificates"
    generate_certificates
    patch_secret
    verify_written_secret
    warn "Certificates rotated successfully; ensure the webhook caBundle is synchronized and restart the admission deployment before relying on the new certificates."
    log "Admission certificates repaired successfully"
}

main "$@"
