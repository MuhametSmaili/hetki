#!/usr/bin/env bash
# shellcheck disable=SC2016 # single-quoted snippets run in nested shells
#
# Installer for hetki
#
#   - installs an exact resolved release tag (never a mutable ref)
#   - always checksum-verifies the download
#   - verifies the release attestation when gh is installed and logged in;
#     warns loudly and falls back to checksum-only otherwise
#   - independently verified alternative without gh:
#       go install github.com/MuhametSmaili/hetki@<tag>
#
# Usage: curl -fsSL https://raw.githubusercontent.com/MuhametSmaili/hetki/main/install.sh | bash
#   HETKI_VERSION=vX.Y.Z   install a specific release tag
#   HETKI_INSTALL_DIR=dir  destination directory (default ~/.local/bin)
#   HETKI_FROM_SOURCE=1    build via go install at the exact resolved tag
#

set -euo pipefail

#######################################
# Constants
#######################################

readonly REPO="MuhametSmaili/hetki"
readonly GO_MODULE="github.com/MuhametSmaili/hetki"
readonly SIGNER_WORKFLOW="${REPO}/.github/workflows/release.yml"
readonly DEFAULT_INSTALL_DIR="${HOME}/.local/bin"
readonly MAX_DOWNLOAD_BYTES=134217728 # 128 MiB
readonly MAX_CHECKSUM_BYTES=65536
readonly MAX_METADATA_BYTES=4194304
readonly MAX_SOURCE_WORKSPACE_KIB=1048576 # 1 GiB aggregate temporary source workspace
readonly API_TIMEOUT_SECONDS=15
readonly DOWNLOAD_TIMEOUT_SECONDS=300
readonly VERIFY_TIMEOUT_SECONDS=30

#######################################
# Global Variables
#######################################

VERSION="${HETKI_VERSION:-latest}"
INSTALL_DIR="${HETKI_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
OS=""
ARCH=""
RESOLVED_TAG=""
RESOLVED_COMMIT=""
LATEST_TAG=""
ROLLBACK_TARGET=""
ROLLBACK_BACKUP=""
REPLACEMENT_LOCK=""
REPLACEMENT_LOCK_OWNER=""
ROLLBACK_BACKUP_ID=""
ORIGINAL_TARGET_ID=""
CANDIDATE_ID=""
REPLACEMENT_PENDING=0
ACTIVE_PGID_FILE=""
SOURCE_WATCH_DIR=""
IDENTITY_VERSION=""
IDENTITY_COMMIT=""
VERIFIED_DIGEST=""

#######################################
# Logging Functions
#######################################

info() {
    printf '\033[0;32m==>\033[0m %s\n' "$1"
}

warn() {
    printf '\033[1;33mWarning:\033[0m %s\n' "$1" >&2
}

error() {
    printf '\033[0;31mError:\033[0m %s\n' "$1" >&2
    exit "${2:-1}"
}

#######################################
# Cleanup
#######################################

declare -a TEMP_FILES=()
ACTIVE_PGID_FILE="$(mktemp)"
TEMP_FILES+=("$ACTIVE_PGID_FILE")

file_id() {
    stat -c '%d:%i' "$1" 2>/dev/null || stat -f '%d:%i' "$1"
}

owned_backup_exists() {
    [[ -n "$ROLLBACK_BACKUP" && -n "$ROLLBACK_BACKUP_ID" && -e "$ROLLBACK_BACKUP" \
        && "$(file_id "$ROLLBACK_BACKUP")" == "$ROLLBACK_BACKUP_ID" ]]
}

wait_for_active_registration() {
    local attempts=0
    while grep -q '^starting$' "$ACTIVE_PGID_FILE" 2>/dev/null && [[ "$attempts" -lt 50 ]]; do
        sleep 0.02
        attempts=$((attempts + 1))
    done
}

terminate_active_groups() {
    local signal="$1" pid
    [[ -f "$ACTIVE_PGID_FILE" ]] || return 0
    while IFS= read -r pid; do
        [[ "$pid" =~ ^[1-9][0-9]*$ ]] && kill -"$signal" -- "-$pid" 2>/dev/null || true
    done <"$ACTIVE_PGID_FILE"
}

cleanup() {
    local temp
    wait_for_active_registration
    terminate_active_groups TERM
    sleep 0.1
    terminate_active_groups KILL
    if [[ "$REPLACEMENT_PENDING" -eq 3 ]]; then
        if [[ -n "$ROLLBACK_BACKUP" && -e "$ROLLBACK_BACKUP" \
            && -e "$ROLLBACK_TARGET" && "$ROLLBACK_TARGET" -ef "$ROLLBACK_BACKUP" ]]; then
            rm -f "$ROLLBACK_BACKUP"
        fi
    elif [[ "$REPLACEMENT_PENDING" -eq 1 || "$REPLACEMENT_PENDING" -eq 4 ]]; then
        if [[ -n "$ROLLBACK_BACKUP" ]]; then
            if owned_backup_exists; then
                if [[ -e "$ROLLBACK_TARGET" && "$(file_id "$ROLLBACK_TARGET")" == "$ORIGINAL_TARGET_ID" ]]; then
                    rm -f "$ROLLBACK_BACKUP"
                elif [[ -e "$ROLLBACK_TARGET" && "$(file_id "$ROLLBACK_TARGET")" == "$CANDIDATE_ID" ]]; then
                    mv -f "$ROLLBACK_BACKUP" "$ROLLBACK_TARGET" \
                        || warn "ROLLBACK FAILED; previous binary remains at $ROLLBACK_BACKUP"
                else
                    warn "Destination identity changed; previous binary remains at $ROLLBACK_BACKUP"
                fi
            elif [[ -e "$ROLLBACK_BACKUP" ]]; then
                warn "Recovery backup ownership changed; left untouched at $ROLLBACK_BACKUP"
            fi
        elif [[ -e "$ROLLBACK_TARGET" && "$(file_id "$ROLLBACK_TARGET")" == "$CANDIDATE_ID" ]]; then
            rm -f "$ROLLBACK_TARGET"
        fi
    elif [[ "$REPLACEMENT_PENDING" -eq 2 ]] && owned_backup_exists; then
        rm -f "$ROLLBACK_BACKUP" || warn "Could not remove verified recovery backup at $ROLLBACK_BACKUP"
    fi
    if [[ -n "$REPLACEMENT_LOCK" && -L "$REPLACEMENT_LOCK" \
        && "$(readlink "$REPLACEMENT_LOCK")" == "$REPLACEMENT_LOCK_OWNER" ]]; then
        rm -f "$REPLACEMENT_LOCK"
    fi
    if [[ -n "$REPLACEMENT_LOCK_OWNER" ]]; then
        rm -rf "$REPLACEMENT_LOCK_OWNER"
    fi
    for temp in "${TEMP_FILES[@]}"; do
        if [[ -e "$temp" ]]; then
            chmod -R u+w "$temp" 2>/dev/null || true
            rm -rf "$temp"
        fi
    done
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

#######################################
# Platform Detection
#######################################

detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$os" in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        *)
            error "Unsupported OS: $os"
            ;;
    esac

    case "$arch" in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64 | arm64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $arch"
            ;;
    esac

    info "Detected platform: $OS/$ARCH"
}

#######################################
# Installation Directory Setup
#######################################

ensure_install_dir() {
    if [[ ! -d "$INSTALL_DIR" ]]; then
        info "Creating installation directory: $INSTALL_DIR"
        if ! mkdir -p "$INSTALL_DIR"; then
            error "Failed to create $INSTALL_DIR. Check permissions."
        fi
    fi

    if [[ ! -w "$INSTALL_DIR" ]]; then
        error "Cannot write to $INSTALL_DIR. Try: HETKI_INSTALL_DIR=~/.local/bin bash install.sh"
    fi
}

check_path() {
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

show_path_instructions() {
    local shell_rc

    case "$SHELL" in
        */zsh)
            shell_rc="$HOME/.zshrc"
            ;;
        */bash)
            shell_rc="$HOME/.bashrc"
            ;;
        *)
            shell_rc="your shell configuration file"
            ;;
    esac

    warn "$INSTALL_DIR is not in your PATH"
    printf '\nAdd this to %s:\n' "$shell_rc"
    printf '  \033[0;34mexport PATH="%s:$PATH"\033[0m\n\n' "$INSTALL_DIR"
    printf 'Then reload your shell:\n'
    printf '  \033[0;34msource %s\033[0m\n\n' "$shell_rc"
}

check_tmux() {
    if ! command -v tmux >/dev/null 2>&1; then
        warn "tmux is not installed. hetki requires tmux."
        printf '\nInstall tmux:\n'
        printf '  macOS:  brew install tmux\n'
        printf '  Ubuntu: sudo apt install tmux\n\n'
    fi
}

#######################################
# Version resolution
#######################################

valid_tag() {
    [[ "$1" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
}

bounded_curl() {
    local max_bytes="$1" timeout="$2"
    shift 2
    run_with_timeout "$timeout" bash -c \
        'ulimit -f "$1"; shift; exec curl "$@" 2>/dev/null' _ \
        "$(( (max_bytes + 1023) / 1024 ))" "$@"
}

api_get() {
    local url="$1" dest="$2" compact
    bounded_curl "$MAX_METADATA_BYTES" "$API_TIMEOUT_SECONDS" -fsSL --max-redirs 5 \
        --connect-timeout "$API_TIMEOUT_SECONDS" --max-time "$API_TIMEOUT_SECONDS" \
        --max-filesize "$MAX_METADATA_BYTES" --proto '=https' --proto-redir '=https' \
        -H 'Accept: application/vnd.github+json' "$url" -o "$dest"
    [[ "$(wc -c < "$dest")" -le "$MAX_METADATA_BYTES" ]] \
        || error "GitHub release metadata exceeds ${MAX_METADATA_BYTES} bytes"
    compact="${dest}.compact"
    tr -d '\r\n' <"$dest" >"$compact"
    mv "$compact" "$dest"
}

decimal_greater() {
    [[ ${#1} -gt ${#2} ]] || { [[ ${#1} -eq ${#2} ]] && [[ "$1" > "$2" ]]; }
}

version_greater() {
    local a_major a_minor a_patch b_major b_minor b_patch
    IFS=. read -r a_major a_minor a_patch <<<"${1#v}"
    IFS=. read -r b_major b_minor b_patch <<<"${2#v}"
    for pair in "$a_major:$b_major" "$a_minor:$b_minor" "$a_patch:$b_patch"; do
        local a="${pair%%:*}" b="${pair#*:}"
        decimal_greater "$a" "$b" && return 0
        decimal_greater "$b" "$a" && return 1
    done
    return 1
}

split_top_level_objects() {
    awk '
    { json = json $0 }
    END {
        depth = 0; in_string = 0; escaped = 0; object = ""
        for (i = 1; i <= length(json); i++) {
            c = substr(json, i, 1)
            if (depth > 0) object = object c
            if (in_string) {
                if (escaped) escaped = 0
                else if (c == "\\") escaped = 1
                else if (c == "\"") in_string = 0
                continue
            }
            if (c == "\"") { in_string = 1; continue }
            if (c == "{") {
                if (depth == 0) object = "{"
                depth++
            } else if (c == "}") {
                depth--
                if (depth == 0) { print object; object = "" }
            }
        }
    }' "$1"
}

json_string_field() {
    local rest="${1#*\""$2"\"}"
    [[ "$rest" != "$1" ]] || return 1
    rest="${rest#*:}"
    rest="${rest#*\"}"
    printf '%s' "${rest%%\"*}"
}

json_bool_field() {
    local rest="${1#*\""$2"\"}" value
    [[ "$rest" != "$1" ]] || return 1
    rest="${rest#*:}"
    rest="${rest#"${rest%%[![:space:]]*}"}"
    rest="${rest%%,*}"
    value="${rest%%\}*}"
    value="${value%"${value##*[![:space:]]}"}"
    printf '%s' "$value"
}

resolve_latest_tag() {
    local metadata="$1" segments count tag line page
    segments="$(mktemp)"
    TEMP_FILES+=("$segments")
    LATEST_TAG=""
    for page in {1..10}; do
        api_get "https://api.github.com/repos/${REPO}/releases?per_page=100&page=${page}" "$metadata"
        split_top_level_objects "$metadata" >"$segments"
        count=0
        while IFS= read -r line || [[ -n "$line" ]]; do
            tag="$(json_string_field "$line" tag_name || true)"
            [[ -n "$tag" ]] || continue
            count=$((count + 1))
            if [[ "$(json_bool_field "$line" draft || true)" == false \
                && "$(json_bool_field "$line" prerelease || true)" == false ]]; then
                valid_tag "$tag" || error "Published stable release tag '$tag' is not vX.Y.Z"
                if [[ -z "$LATEST_TAG" ]] || version_greater "$tag" "$LATEST_TAG"; then
                    LATEST_TAG="$tag"
                fi
            fi
        done <"$segments"
        [[ "$count" -lt 100 ]] && break
        [[ "$page" -lt 10 ]] || error "Release history exceeds scan limit of 1000"
    done
    [[ -n "$LATEST_TAG" ]] || error "No stable vX.Y.Z release found"
}

# resolve_tag sets the exact tag and commit to install. There is no mutable-ref
# or default-branch fallback.
resolve_tag() {
    local tag metadata release
    metadata="$(mktemp)"
    TEMP_FILES+=("$metadata")
    if [[ "$VERSION" == "latest" ]]; then
        resolve_latest_tag "$metadata"
        tag="$LATEST_TAG"
    else
        valid_tag "$VERSION" || error "HETKI_VERSION must be a vX.Y.Z release tag (got '$VERSION')"
        api_get "https://api.github.com/repos/${REPO}/releases/tags/$VERSION" "$metadata"
        release="$(split_top_level_objects "$metadata" | head -1)"
        tag="$(json_string_field "$release" tag_name || true)"
        [[ "$tag" == "$VERSION" ]] || error "GitHub returned release tag '$tag', expected '$VERSION'"
        [[ "$(json_bool_field "$release" draft || true)" == false ]] || error "Release '$tag' is a draft"
        [[ "$(json_bool_field "$release" prerelease || true)" == false ]] \
            || error "Release '$tag' is marked as a prerelease; the installer supports stable releases only"
    fi
    RESOLVED_TAG="$tag"
    resolve_commit "$tag"
}

valid_commit() {
    [[ "$1" =~ ^[0-9a-f]{40}$|^[0-9a-f]{64}$ ]]
}

resolve_commit() {
    local tag="$1" metadata sha type depth=0
    metadata="$(mktemp)"
    TEMP_FILES+=("$metadata")
    api_get "https://api.github.com/repos/${REPO}/git/ref/tags/${tag}" "$metadata"
    sha="$(sed -n 's/.*"object"[[:space:]]*:[[:space:]]*{[^}]*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p' "$metadata")"
    type="$(sed -n 's/.*"object"[[:space:]]*:[[:space:]]*{[^}]*"type"[[:space:]]*:[[:space:]]*"\([a-z]*\)".*/\1/p' "$metadata")"
    while [[ "$type" == "tag" && "$depth" -lt 5 ]]; do
        api_get "https://api.github.com/repos/${REPO}/git/tags/${sha}" "$metadata"
        sha="$(sed -n 's/.*"object"[[:space:]]*:[[:space:]]*{[^}]*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p' "$metadata")"
        type="$(sed -n 's/.*"object"[[:space:]]*:[[:space:]]*{[^}]*"type"[[:space:]]*:[[:space:]]*"\([a-z]*\)".*/\1/p' "$metadata")"
        depth=$((depth + 1))
    done
    if [[ "$type" != "commit" ]] || ! valid_commit "$sha"; then
        error "Release tag '$tag' does not resolve to a valid commit"
    fi
    RESOLVED_COMMIT="$sha"
}

#######################################
# Download and verification
#######################################

download_file() {
    local url="$1" dest="$2" max_bytes="$3"

    bounded_curl "$max_bytes" "$DOWNLOAD_TIMEOUT_SECONDS" -fsSL \
        --max-redirs 5 \
        --connect-timeout "$API_TIMEOUT_SECONDS" \
        --max-time "$DOWNLOAD_TIMEOUT_SECONDS" \
        --max-filesize "$max_bytes" \
        --proto '=https' --proto-redir '=https' \
        "$url" -o "$dest" || return 1
    [[ "$(wc -c < "$dest")" -le "$max_bytes" ]]
}

# verify_checksum always runs: it is integrity, not authenticity, and needs
# nothing beyond sha256sum/shasum.
verify_checksum() {
    local binary="$1"
    local checksums="$2"
    local binary_name="$3"
    local expected_hash actual_hash

    expected_hash="$(run_with_timeout "$VERIFY_TIMEOUT_SECONDS" bash -c \
        'awk -v name="$1" '\''$2 == name { print $1; exit }'\'' "$2" | head -c 129' \
        _ "$binary_name" "$checksums")"
    [[ -n "$expected_hash" ]] || error "Could not find checksum for ${binary_name} in checksums.txt"

    if command -v sha256sum >/dev/null 2>&1; then
        actual_hash="$(run_with_timeout "$VERIFY_TIMEOUT_SECONDS" bash -c \
            'set -o pipefail; sha256sum "$1" | awk '\''{print $1}'\'' | head -c 129' _ "$binary")"
    elif command -v shasum >/dev/null 2>&1; then
        actual_hash="$(run_with_timeout "$VERIFY_TIMEOUT_SECONDS" bash -c \
            'set -o pipefail; shasum -a 256 "$1" | awk '\''{print $1}'\'' | head -c 129' _ "$binary")"
    else
        error "Neither sha256sum nor shasum is available; cannot verify the download"
    fi

    if [[ "$actual_hash" != "$expected_hash" ]]; then
        error "Checksum verification failed!\n  Expected: ${expected_hash}\n  Actual:   ${actual_hash}"
    fi

    VERIFIED_DIGEST="$actual_hash"
    info "Checksum verified: ${actual_hash}"
}

run_with_timeout() (
    local seconds="$1"
    shift
    set -m
    printf 'starting\n' >"$ACTIVE_PGID_FILE"
    "$@" &
    local pid=$! watchdog="" resource_watchdog="" status size
    printf '%s\n' "$pid" >"$ACTIVE_PGID_FILE"
    terminate_groups() {
        local signal="$1" group
        for group in "$pid" "$watchdog" "$resource_watchdog"; do
            [[ "$group" =~ ^[1-9][0-9]*$ ]] && kill -"$signal" -- "-$group" 2>/dev/null || true
        done
    }
    trap 'terminate_groups TERM; sleep 0.1; terminate_groups KILL; exit 130' INT
    trap 'terminate_groups TERM; sleep 0.1; terminate_groups KILL; exit 143' TERM
    (sleep "$seconds"; kill -TERM -- "-$pid" 2>/dev/null || true; sleep 2; kill -KILL -- "-$pid" 2>/dev/null || true) \
        >/dev/null 2>&1 &
    watchdog=$!
    printf '%s\n' "$watchdog" >>"$ACTIVE_PGID_FILE"
    if [[ -n "$SOURCE_WATCH_DIR" ]]; then
        (while kill -0 "$pid" 2>/dev/null; do
            size="$(du -sk "$SOURCE_WATCH_DIR" 2>/dev/null | awk '{print $1}')"
            if [[ ! "$size" =~ ^[0-9]+$ || "$size" -gt "$MAX_SOURCE_WORKSPACE_KIB" ]]; then
                kill -TERM -- "-$pid" 2>/dev/null || true
                sleep 2
                kill -KILL -- "-$pid" 2>/dev/null || true
                exit
            fi
            sleep 1
        done) >/dev/null 2>&1 &
        resource_watchdog=$!
        printf '%s\n' "$resource_watchdog" >>"$ACTIVE_PGID_FILE"
    fi
    if wait "$pid"; then status=0; else status=$?; fi
    terminate_groups TERM
    sleep 0.1
    terminate_groups KILL
    [[ -n "$watchdog" ]] && wait "$watchdog" 2>/dev/null || true
    [[ -n "$resource_watchdog" ]] && wait "$resource_watchdog" 2>/dev/null || true
    : >"$ACTIVE_PGID_FILE"
    set +m
    return "$status"
)

gh_supports_attestation() {
    local output major minor
    output="$(run_with_timeout "$API_TIMEOUT_SECONDS" bash -c \
        'set -o pipefail; gh --version 2>&1 | head -c 4097')" || return 1
    [[ "$(printf '%s' "$output" | wc -c)" -le 4096 ]] || return 1
    [[ "$output" =~ gh[[:space:]]version[[:space:]]([0-9]+)\.([0-9]+) ]] || return 1
    major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}"
    # 2.102 fixes signer-workflow prefix matching and case-insensitive source-ref.
    [[ "$major" -gt 2 || ( "$major" -eq 2 && "$minor" -ge 102 ) ]]
}

# Online gh verification is optional at bootstrap, but any attempted check
# is bounded and must pass.
verify_attestation() {
    local binary="$1" tag="$2" commit="$3" binary_name="$4" result jq_filter

    if ! command -v gh >/dev/null 2>&1 || ! gh_supports_attestation \
        || ! run_with_timeout "$API_TIMEOUT_SECONDS" gh auth status >/dev/null 2>&1; then
        warn "gh CLI not available or not logged in: authenticity NOT independently verified (checksums only)."
        warn "Verified alternative: go install ${GO_MODULE}@${tag}"
        return 0
    fi

    info "Verifying release attestation..."
    result="$(mktemp)"
    TEMP_FILES+=("$result")
    jq_filter="[.[] | .verificationResult.statement.subject[] | select(.name == \"${binary_name}\" and .digest.sha256 == \"${VERIFIED_DIGEST}\") | .name][0] // empty"
    if ! run_with_timeout "$DOWNLOAD_TIMEOUT_SECONDS" bash -c \
        'set -o pipefail; "$@" 2>&1 | head -c 4097' _ gh attestation verify "$binary" --repo "$REPO" \
        --signer-workflow "$SIGNER_WORKFLOW" --source-ref "refs/tags/${tag}" \
        --source-digest "$commit" --deny-self-hosted-runners --format json --jq "$jq_filter" >"$result"; then
        error "Attestation verification failed; refusing to install"
    fi
    [[ "$(wc -c < "$result")" -le 4096 ]] || error "Attestation verification output exceeded its bound"
    [[ "$(cat "$result")" == "$binary_name" ]] \
        || error "Verified attestation does not name ${binary_name} with its downloaded digest"
    info "Attestation verified against ${SIGNER_WORKFLOW}"
}

#######################################
# Installation Methods
#######################################

read_identity() {
    local binary="$1" output
    output="$(mktemp)"
    TEMP_FILES+=("$output")
    run_with_timeout "$VERIFY_TIMEOUT_SECONDS" bash -c \
        'ulimit -f 8; exec "$1" --version' _ "$binary" >"$output" 2>/dev/null || return $?
    IDENTITY_VERSION="$(sed -n '1s/^hetki version //p' "$output")"
    IDENTITY_COMMIT="$(sed -n '2s/^commit: //p' "$output")"
}

install_verified_binary() {
    local binary="$1" target_path="$2" tag="$3" commit="$4" backup_path="${2}.hetki-backup"
    local lock_path="${2}.hetki-update-lock" lock_owner target_mode=0755 target_owner="" candidate_owner had_previous=0 file_size

    [[ -f "$binary" && ! -L "$binary" ]] || error "Install candidate must be a regular, non-symlink file"
    file_size="$(wc -c < "$binary")"
    [[ "$file_size" -gt 0 && "$file_size" -le "$MAX_DOWNLOAD_BYTES" ]] \
        || error "Install candidate size ${file_size} is outside the 1-${MAX_DOWNLOAD_BYTES} byte bound"
    CANDIDATE_ID="$(file_id "$binary")"
    [[ ! -L "$target_path" ]] || error "Refusing to replace symlinked installation: ${target_path}"
    [[ ! -e "$target_path" || -f "$target_path" ]] \
        || error "Refusing to replace non-regular installation: ${target_path}"
    [[ ! -e "$backup_path" && ! -L "$backup_path" ]] \
        || error "Recovery backup already exists at ${backup_path}; inspect it before updating"
    lock_owner="${lock_path}.$$.${RANDOM}"
    REPLACEMENT_LOCK="$lock_path"
    REPLACEMENT_LOCK_OWNER="$lock_owner"
    mkdir -m 700 "$lock_owner" || error "Could not create installer lock owner at ${lock_owner}"
    if ! ln -s "$lock_owner" "$lock_path" 2>/dev/null; then
        error "Another installation is active or left a lock at ${lock_path}"
    fi
    ROLLBACK_TARGET="$target_path"
    if [[ -e "$target_path" ]]; then
        had_previous=1
        ROLLBACK_BACKUP="$backup_path"
        ORIGINAL_TARGET_ID="$(file_id "$target_path")"
        REPLACEMENT_PENDING=3
        target_mode="$(stat -c '%a' "$target_path" 2>/dev/null || stat -f '%Mp%Lp' "$target_path")"
        target_owner="$(stat -c '%u:%g' "$target_path" 2>/dev/null || stat -f '%u:%g' "$target_path")"
        if ! ln "$target_path" "$backup_path"; then
            error "Could not create exclusive recovery backup at ${backup_path}"
        fi
        if [[ -L "$backup_path" ]]; then
            rm -f "$backup_path"
            error "Installation became a symlink while creating its backup"
        fi
        ROLLBACK_BACKUP_ID="$(file_id "$backup_path")"
        [[ "$ROLLBACK_BACKUP_ID" == "$ORIGINAL_TARGET_ID" ]] \
            || error "Installation changed while creating its recovery backup"
        REPLACEMENT_PENDING=1
    fi
    if [[ -n "$target_owner" ]]; then
        candidate_owner="$(stat -c '%u:%g' "$binary" 2>/dev/null || stat -f '%u:%g' "$binary")"
        if [[ "$candidate_owner" != "$target_owner" ]]; then
            command -v chown >/dev/null 2>&1 || error "chown is required to preserve installation ownership"
            chown "$target_owner" "$binary" || error "Could not preserve installation ownership"
        fi
    fi
    chmod "$target_mode" "$binary"

    [[ ! -L "$target_path" ]] || error "Installation became a symlink during update: ${target_path}"
    if [[ "$had_previous" -eq 0 ]]; then
        ROLLBACK_BACKUP=""
        ORIGINAL_TARGET_ID=""
        REPLACEMENT_PENDING=1
        [[ ! -e "$target_path" && ! -L "$target_path" ]] \
            || error "Installation destination appeared before replacement; refusing to overwrite it"
    else
        [[ -f "$target_path" && ! -L "$target_path" \
            && "$(file_id "$target_path")" == "$ORIGINAL_TARGET_ID" ]] \
            || error "Installation destination changed before replacement; refusing to overwrite it"
        owned_backup_exists || error "Recovery backup identity changed before replacement"
    fi
    [[ -f "$binary" && ! -L "$binary" && "$(file_id "$binary")" == "$CANDIDATE_ID" ]] \
        || error "Install candidate identity changed before replacement"
    if ! mv "$binary" "$target_path"; then
        error "Failed to install to ${target_path}"
    fi
    REPLACEMENT_PENDING=4

    if ! read_identity "$target_path" \
        || [[ ! -e "$target_path" || "$(file_id "$target_path")" != "$CANDIDATE_ID" ]] \
        || [[ "$IDENTITY_VERSION" != "$tag" || "$IDENTITY_COMMIT" != "$commit" ]]; then
        error "Installed binary reports ${IDENTITY_VERSION:-unknown} at ${IDENTITY_COMMIT:-unknown}, expected ${tag} at ${commit}; rolling back"
    fi
    REPLACEMENT_PENDING=2
    if [[ -n "$ROLLBACK_BACKUP" ]]; then
        owned_backup_exists || error "Recovery backup identity changed after verification"
        rm -f "$backup_path"
    fi
    REPLACEMENT_PENDING=0
}

verify_source_origin() {
    local tag="$1" commit="$2" mod_cache="$3" source_root="${3%/*}" metadata path version sum url hash ref
    metadata="$(mktemp)"
    TEMP_FILES+=("$metadata")
    run_with_timeout "$DOWNLOAD_TIMEOUT_SECONDS" env GOMODCACHE="$mod_cache" \
        GOCACHE="$source_root/gocache" GOTMPDIR="$source_root/tmp" GOPROXY=direct \
        GOSUMDB=sum.golang.org GONOSUMDB= GOPRIVATE= GONOPROXY= \
        bash -c 'set -o pipefail; go mod download -json "$1" 2>&1 | head -c 1048577' _ "${GO_MODULE}@${tag}" >"$metadata" \
        || error "Timed out or failed while verifying source origin"
    [[ "$(wc -c < "$metadata")" -le 1048576 ]] || error "Source provenance output exceeded its bound"
    path="$(sed -n 's/[[:space:]]*"Path": "\([^"]*\)".*/\1/p' "$metadata")"
    version="$(sed -n 's/[[:space:]]*"Version": "\([^"]*\)".*/\1/p' "$metadata")"
    sum="$(sed -n 's/[[:space:]]*"Sum": "\([^"]*\)".*/\1/p' "$metadata")"
    url="$(sed -n '/"Origin"/,/}/s/[[:space:]]*"URL": "\([^"]*\)".*/\1/p' "$metadata")"
    hash="$(sed -n '/"Origin"/,/}/s/[[:space:]]*"Hash": "\([^"]*\)".*/\1/p' "$metadata")"
    ref="$(sed -n '/"Origin"/,/}/s/[[:space:]]*"Ref": "\([^"]*\)".*/\1/p' "$metadata")"
    [[ "$path" == "$GO_MODULE" && "$version" == "$tag" && -n "$sum" \
        && "$url" == "https://github.com/MuhametSmaili/hetki" && "$hash" == "$commit" \
        && "$ref" == "refs/tags/$tag" ]] \
        || error "Go module origin does not match ${tag} at ${commit}"
}

source_workspace_kib() {
    local size
    size="$(du -sk "$1" 2>/dev/null | awk '{print $1}')" \
        || error "Could not measure source workspace"
    [[ "$size" =~ ^[0-9]+$ ]] || error "Could not measure source workspace"
    printf '%s' "$size"
}

install_from_source() {
    local tag temp_dir target_path mod_cache build_log ldflags workspace_kib

    command -v go >/dev/null 2>&1 || error "Source install requires go; install it or use the release binary"

    resolve_tag
    tag="$RESOLVED_TAG"
    temp_dir="$(mktemp -d "${INSTALL_DIR}/.hetki-source-XXXXXX")"
    TEMP_FILES+=("$temp_dir")
    target_path="${INSTALL_DIR}/hetki"
    mod_cache="$temp_dir/modcache"
    mkdir -p "$temp_dir/gocache" "$temp_dir/tmp"
    build_log="$(mktemp)"
    TEMP_FILES+=("$build_log")
    ldflags="-X ${GO_MODULE}/cmd.Version=${tag} -X ${GO_MODULE}/cmd.GitCommit=${RESOLVED_COMMIT}"
    info "Installing from source at exact tag ${tag} (verified by the Go checksum database)..."
    SOURCE_WATCH_DIR="$temp_dir"
    verify_source_origin "$tag" "$RESOLVED_COMMIT" "$mod_cache"
    workspace_kib="$(source_workspace_kib "$temp_dir")"
    [[ "$workspace_kib" -le "$MAX_SOURCE_WORKSPACE_KIB" ]] || error "Source workspace exceeded 1 GiB"
    if ! run_with_timeout "$DOWNLOAD_TIMEOUT_SECONDS" env GOBIN="$temp_dir" GOMODCACHE="$mod_cache" \
        GOCACHE="$temp_dir/gocache" GOTMPDIR="$temp_dir/tmp" GOPROXY=direct \
        GOSUMDB=sum.golang.org GONOSUMDB= GOPRIVATE= GONOPROXY= \
        bash -c 'set -o pipefail; ulimit -f 131072; go install -ldflags "$1" "$2" 2>&1 | head -c 1048577' _ "$ldflags" "${GO_MODULE}@${tag}" \
        >"$build_log"; then
        tail -20 "$build_log" >&2
        error "Source build timed out or failed"
    fi
    [[ "$(wc -c < "$build_log")" -le 1048576 ]] || error "Source build output exceeded its bound"
    workspace_kib="$(source_workspace_kib "$temp_dir")"
    [[ "$workspace_kib" -le "$MAX_SOURCE_WORKSPACE_KIB" ]] || error "Source workspace exceeded 1 GiB"
    [[ -f "$temp_dir/hetki" && ! -L "$temp_dir/hetki" ]] \
        || error "go install did not produce a regular, non-symlink hetki binary"
    SOURCE_WATCH_DIR=""
    install_verified_binary "$temp_dir/hetki" "$target_path" "$tag" "$RESOLVED_COMMIT"
    info "Installed and verified ${target_path} at ${tag}"
}

install_from_release() {
    local tag binary_name base_url temp_file temp_checksums target_path
    local file_size

    resolve_tag
    tag="$RESOLVED_TAG"
    binary_name="hetki-${OS}-${ARCH}"
    base_url="https://github.com/${REPO}/releases/download/${tag}"
    target_path="${INSTALL_DIR}/hetki"

    info "Downloading hetki ${tag} for ${OS}/${ARCH}..."

    temp_file="$(mktemp "${INSTALL_DIR}/.hetki-install-XXXXXX")"
    TEMP_FILES+=("$temp_file")
    temp_checksums="$(mktemp)"
    TEMP_FILES+=("$temp_checksums")

    download_file "${base_url}/${binary_name}" "$temp_file" "$MAX_DOWNLOAD_BYTES" \
        || error "No release binary ${binary_name} for ${tag}, or artifact exceeds ${MAX_DOWNLOAD_BYTES} bytes"
    download_file "${base_url}/checksums.txt" "$temp_checksums" "$MAX_CHECKSUM_BYTES" \
        || error "Could not download bounded checksums.txt for ${tag}"

    # Sanity check: Go binaries are typically 5-15MB; reject tiny files
    # (catches HTML error pages, truncated downloads, etc.)
    file_size="$(wc -c < "$temp_file")"
    if [[ "$file_size" -lt 1048576 ]]; then
        error "Downloaded file is too small (${file_size} bytes). Expected a Go binary (>1MB)."
    fi

    verify_checksum "$temp_file" "$temp_checksums" "$binary_name"
    verify_attestation "$temp_file" "$tag" "$RESOLVED_COMMIT" "$binary_name"

    install_verified_binary "$temp_file" "$target_path" "$tag" "$RESOLVED_COMMIT"
    info "Installed and verified ${target_path} at ${tag}"
}

#######################################
# Verification
#######################################

show_post_install_info() {
    printf '\nGet started:\n'
    printf '  hetki              # Browse sessions\n'
    printf '  hetki save work    # Save the current session from inside tmux\n'
    printf '  hetki start work   # Start a saved workspace\n'
    printf '\nFor more info: hetki --help\n'
}

#######################################
# Main
#######################################

main() {
    detect_platform
    ensure_install_dir
    check_tmux

    if [[ -n "${HETKI_FROM_SOURCE:-}" ]]; then
        install_from_source
    else
        install_from_release
    fi

    show_post_install_info

    if ! check_path; then
        show_path_instructions
    fi
}

main "$@"
