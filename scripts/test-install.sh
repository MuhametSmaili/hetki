#!/usr/bin/env bash
#
# Executable harness for install.sh with stubbed network (curl), gh, and a
# sandboxed destination. Runs the installer as a subprocess against fixture
# releases; no real network, no real gh, no real home.
#
# Usage: ./scripts/test-install.sh

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INSTALLER="$ROOT/install.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAIL=0

note() { printf '  %s\n' "$1"; }

check() {
    local name="$1" condition="$2"
    if eval "$condition"; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        printf 'FAIL: %s (condition: %s)\n' "$name" "$condition" >&2
    fi
}

report() {
    printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
    [[ "$FAIL" -eq 0 ]]
}

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64 | arm64) ARCH="arm64" ;;
esac
BINARY_NAME="hetki-${OS}-${ARCH}"
COMMIT="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

FIXTURES="$WORK/fixtures"
mkdir -p "$FIXTURES"

# A fake release "binary": a script reporting a version, padded over the
# installer's 1MiB sanity floor.
make_fake_binary() {
    local path="$1" version="$2"
    {
        printf '#!/bin/sh\n'
        printf "printf 'hetki version %s\\ncommit: %s\\n'\n" "$version" "$COMMIT"
        printf '#%s\n' "$(head -c 1048576 /dev/zero | tr '\0' 'x')"
    } >"$path"
    chmod +x "$path"
}

sha() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

make_fake_binary "$FIXTURES/$BINARY_NAME" "v1.0.0"
make_fake_binary "$FIXTURES/hetki-liar-$OS-$ARCH" "v9.9.9"
printf '[\n  {\n    "tag_name": "v1.0.0",\n    "draft": false,\n    "prerelease": false\n  }\n]\n' >"$FIXTURES/latest.json"
printf '{\n  "tag_name": "v1.0.0",\n  "draft": false,\n  "prerelease": false\n}\n' >"$FIXTURES/tag.json"
printf '{\n  "sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",\n  "object": {\n    "sha": "%s",\n    "type": "commit"\n  }\n}\n' "$COMMIT" >"$FIXTURES/ref.json"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums-tampered.txt"
# Tampered checksums: hash of a different file under the right name.
printf '%s  %s\n' "$(sha "$FIXTURES/latest.json")" "$BINARY_NAME" >"$FIXTURES/checksums-wrong.txt"
: >"$FIXTURES/truncated" # under the 1MiB floor

# stub curl: serves fixtures by URL suffix; -o dest supported; records hits.
write_stub_curl() {
    local dir="$1"
    cat >"$dir/curl" <<'EOF'
#!/bin/bash
printf '%s\n' "$*" >>"${HETKI_TEST_CURL_ARGS_LOG:-/dev/null}"
url="" dest=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) dest="$2"; shift 2 ;;
        -*) shift ;;
        *) url="$1"; shift ;;
    esac
done
printf '%s\n' "$url" >>"${HETKI_TEST_CURL_LOG:-/dev/null}"
if [[ -n "${HETKI_TEST_CURL_PID_FILE:-}" ]]; then
    printf '%s\n' "$$" >"$HETKI_TEST_CURL_PID_FILE"
    sleep 30
fi
case "$url" in
    */releases/latest | */releases\?*)
        [[ -n "$dest" ]] && cp "${HETKI_TEST_FIXTURES}/latest.json" "$dest" || cat "${HETKI_TEST_FIXTURES}/latest.json"
        ;;
    */releases/tags/*)
        [[ -n "$dest" ]] && cp "${HETKI_TEST_FIXTURES}/tag.json" "$dest" || cat "${HETKI_TEST_FIXTURES}/tag.json"
        ;;
    */git/ref/tags/*)
        [[ -n "$dest" ]] && cp "${HETKI_TEST_FIXTURES}/ref.json" "$dest" || cat "${HETKI_TEST_FIXTURES}/ref.json"
        ;;
    */checksums.txt)
        [[ -n "$dest" ]] && cp "${HETKI_TEST_FIXTURES}/${HETKI_TEST_CHECKSUMS:-checksums.txt}" "$dest"
        ;;
    *)
        name="${url##*/}"
        if [[ -f "${HETKI_TEST_FIXTURES}/$name" ]]; then
            [[ -n "$dest" ]] && cp "${HETKI_TEST_FIXTURES}/$name" "$dest"
        else
            printf 'stub curl: not found %s\n' "$url" >&2
            exit 22
        fi
        ;;
esac
EOF
    chmod +x "$dir/curl"
}

# stub gh: modes ok | fail | none. ok/fail record attestation calls.
write_stub_go() {
    local dir="$1"
    cat >"$dir/go" <<'EOF'
#!/bin/bash
printf 'GOPROXY=%s GOSUMDB=%s GONOSUMDB=%s GOPRIVATE=%s GONOPROXY=%s GOMODCACHE=%s GOCACHE=%s GOTMPDIR=%s ARGS=%s\n' \
    "$GOPROXY" "$GOSUMDB" "$GONOSUMDB" "$GOPRIVATE" "$GONOPROXY" "$GOMODCACHE" "$GOCACHE" "$GOTMPDIR" "$*" >>"${HETKI_TEST_GO_LOG:-/dev/null}"
if [[ "$1 $2" == "mod download" ]]; then
    printf '{\n  "Path": "github.com/MuhametSmaili/hetki",\n  "Version": "v1.0.0",\n  "Sum": "h1:test",\n  "Origin": {\n    "VCS": "git",\n    "URL": "https://github.com/MuhametSmaili/hetki",\n    "Hash": "%s",\n    "Ref": "refs/tags/v1.0.0"\n  }\n}\n' "$HETKI_TEST_COMMIT"
    exit 0
fi
mkdir -p "$GOBIN"
cat >"$GOBIN/hetki" <<SCRIPT
#!/bin/sh
printf 'hetki version v1.0.0\ncommit: ${HETKI_TEST_COMMIT}\n'
SCRIPT
chmod +x "$GOBIN/hetki"
EOF
    chmod +x "$dir/go"
}

write_stub_gh() {
    local dir="$1" mode="$2"
    [[ "$mode" == "none" ]] && return 0
    cat >"$dir/gh" <<EOF
#!/bin/bash
if [[ "\$1" == "--version" ]]; then echo 'gh version 2.102.0 (test)'; exit 0; fi
if [[ "\$1" == "auth" ]]; then exit 0; fi
if [[ "\$1" == "attestation" ]]; then
    printf '%s\n' "\$*" >>"\${HETKI_TEST_GH_LOG:-/dev/null}"
    args=" \$* "
    for required in '--repo MuhametSmaili/hetki' '--signer-workflow MuhametSmaili/hetki/.github/workflows/release.yml' '--source-ref refs/tags/v1.0.0' '--source-digest aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' '--deny-self-hosted-runners' '--format json' '--jq' '.verificationResult.statement.subject[]' '.digest.sha256'; do
        case "\$args" in *" \$required "*) ;; *) printf 'missing required attestation flag: %s\n' "\$required" >&2; exit 2 ;; esac
    done
    if [[ "$mode" == "ok" || "$mode" == "wrong-subject" ]]; then
        name="\$HETKI_TEST_BINARY_NAME"; [[ "$mode" == "wrong-subject" ]] && name=other
        printf '%s\n' "\$name"
        exit 0
    fi
    printf 'no attestations found\n' >&2
    exit 1
fi
exit 0
EOF
    chmod +x "$dir/gh"
}

# run_case NAME GH_MODE [VERSION] [PRESEED_VERSION] -- runs the installer in
# a sandbox. PRESEED_VERSION installs an existing hetki first (upgrade/rollback
# cases). Sets: CASE_DIR (sandbox), BIN (installed hetki path).
run_case() {
    local name="$1" gh_mode="$2" version="${3:-latest}" preseed="${4:-}"
    CASE_DIR="$WORK/$name"
    HETKI_TEST_HOME="$CASE_DIR/home"
    mkdir -p "$HETKI_TEST_HOME"
    STUBS="$CASE_DIR/stubs"
    mkdir -p "$STUBS"
    write_stub_curl "$STUBS"
    write_stub_gh "$STUBS" "$gh_mode"
    write_stub_go "$STUBS"
    HETKI_INSTALL_DIR="$HETKI_TEST_HOME/.local/bin"
    BIN="$HETKI_INSTALL_DIR/hetki"
    if [[ -n "$preseed" ]]; then
        mkdir -p "$HETKI_INSTALL_DIR"
        if [[ "${HETKI_TEST_PRESEED_FIFO:-0}" == 1 ]]; then
            mkfifo "$BIN"
            chmod 0755 "$BIN"
        elif [[ "${HETKI_TEST_PRESEED_SYMLINK:-0}" == 1 ]]; then
            make_fake_binary "$HETKI_INSTALL_DIR/real-hetki" "$preseed"
            ln -s "$HETKI_INSTALL_DIR/real-hetki" "$BIN"
        else
            make_fake_binary "$BIN" "$preseed"
            chmod "${HETKI_TEST_PRESEED_MODE:-0755}" "$BIN"
        fi
    fi
    HETKI_TEST_CURL_LOG="$CASE_DIR/curl.log"
    HETKI_TEST_GH_LOG="$CASE_DIR/gh.log"
    HETKI_TEST_GO_LOG="$CASE_DIR/go.log"
    HETKI_TEST_CURL_ARGS_LOG="$CASE_DIR/curl-args.log"
    if [[ -n "${HETKI_TEST_INSTALL_DIR_MODE:-}" ]]; then
        chmod "$HETKI_TEST_INSTALL_DIR_MODE" "$HETKI_INSTALL_DIR"
    fi
    : >"$HETKI_TEST_CURL_LOG"
    : >"$HETKI_TEST_GH_LOG"
    : >"$HETKI_TEST_GO_LOG"
    # PATH contains only stubs + core tools, so no real gh can leak in.
    env -i \
        PATH="$STUBS:/usr/bin:/bin" \
        HOME="$HETKI_TEST_HOME" \
        HETKI_VERSION="$version" \
        HETKI_INSTALL_DIR="$HETKI_INSTALL_DIR" \
        HETKI_TEST_FIXTURES="$FIXTURES" \
        HETKI_TEST_CHECKSUMS="${HETKI_TEST_CHECKSUMS:-checksums.txt}" \
        HETKI_TEST_CURL_LOG="$HETKI_TEST_CURL_LOG" \
        HETKI_TEST_CURL_ARGS_LOG="$HETKI_TEST_CURL_ARGS_LOG" \
        HETKI_TEST_GH_LOG="$HETKI_TEST_GH_LOG" \
        HETKI_TEST_GO_LOG="$HETKI_TEST_GO_LOG" \
        HETKI_TEST_COMMIT="$COMMIT" \
        HETKI_TEST_BINARY_NAME="$BINARY_NAME" \
        HETKI_FROM_SOURCE="${HETKI_FROM_SOURCE:-}" \
        bash "$INSTALLER" >"$CASE_DIR/out.log" 2>"$CASE_DIR/err.log"
    return $?
}

# Used only under set -e discipline inside checks.
try() { "$@" >/dev/null 2>&1; }

echo "== latest release, gh verified =="
if run_case happy ok; then
    check "binary installed" "[[ -x \"\$BIN\" ]]"
    check "reports v1.0.0" "[[ \"\$(\"\$BIN\" --version)\" == *'version v1.0.0'* ]]"
    check "attestation verified" "grep -q 'attestation verify' \"\$HETKI_TEST_GH_LOG\""
    check "curl controls asserted" "grep -q -- '--max-redirs 5.*--max-filesize.*--proto =https' \"\$WORK/happy/curl-args.log\""
    check "no backup left" "[[ ! -e \"\$BIN.hetki-backup\" ]]"
    check "no temp files left" "[[ \$(ls -A \"\$HETKI_INSTALL_DIR\" | wc -l) -eq 1 ]]"
else
    FAIL=$((FAIL + 1)); note "installer failed: $(tail -3 "$WORK/happy/err.log")"
fi

echo "== attestation failure refuses to install =="
if run_case attest-fail fail; then
    FAIL=$((FAIL + 1)); note "installer should have failed"
else
    check "no binary installed" "[[ ! -e \"\$BIN\" ]]"
    check "failure mentions attestation" "grep -qi 'attestation' \"\$WORK/attest-fail/err.log\""
fi

echo "== wrong attestation subject refuses to install =="
if run_case wrong-subject wrong-subject; then
    FAIL=$((FAIL + 1)); note "installer should reject the wrong attestation subject"
else
    check "wrong subject rejected" "grep -q 'does not name' \"\$WORK/wrong-subject/err.log\""
fi

echo "== no gh falls back to checksums with warning =="
if run_case no-gh none; then
    check "binary installed" "[[ -x \"\$BIN\" ]]"
    check "warns about authenticity" "grep -q 'NOT independently verified' \"\$WORK/no-gh/err.log\""
    check "no attestation call" "[[ ! -s \"\$WORK/no-gh/gh.log\" ]]"
else
    FAIL=$((FAIL + 1)); note "installer failed: $(tail -3 "$WORK/no-gh/err.log")"
fi

echo "== tampered checksum refuses to install =="
HETKI_TEST_CHECKSUMS=checksums-wrong.txt run_case tampered ok 2>/dev/null
tampered_status=$?
if [[ $tampered_status -eq 0 ]]; then
    FAIL=$((FAIL + 1)); note "installer should have failed"
else
    check "no binary installed" "[[ ! -e \"\$BIN\" ]]"
    check "failure mentions checksum" "grep -q -i 'checksum' \"\$WORK/tampered/err.log\""
fi

echo "== truncated download refuses to install =="
# Replace the platform fixture with a truncated file and matching checksums so
# the failure comes from the size floor, not the checksum gate.
cp "$FIXTURES/$BINARY_NAME" "$FIXTURES/hetki-good"
: >"$FIXTURES/$BINARY_NAME"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"
if run_case truncated ok; then
    FAIL=$((FAIL + 1)); note "installer should have failed"
else
    check "no binary installed" "[[ ! -e \"\$BIN\" ]]"
    check "failure mentions too small" "grep -q 'too small' \"\$WORK/truncated/err.log\""
fi
mv "$FIXTURES/hetki-good" "$FIXTURES/$BINARY_NAME"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"

echo "== exact version uses the tag endpoint =="
if run_case exact ok v1.0.0; then
    check "tag endpoint queried" "grep -q 'releases/tags/v1.0.0' \"\$WORK/exact/curl.log\""
    check "binary installed" "[[ -x \"\$BIN\" ]]"
else
    FAIL=$((FAIL + 1)); note "installer failed: $(tail -3 "$WORK/exact/err.log")"
fi

echo "== invalid HETKI_VERSION fails =="
if run_case bad-version ok v1; then
    FAIL=$((FAIL + 1)); note "installer should have failed"
else
    check "explains tag format" "grep -q 'vX.Y.Z' \"\$WORK/bad-version/err.log\""
fi
if run_case bad-version-suffix ok v1.2.3evil; then
    FAIL=$((FAIL + 1)); note "installer should reject a tag suffix"
else
    check "strict tag rejects suffix" "grep -q 'vX.Y.Z' \"\$WORK/bad-version-suffix/err.log\""
fi

echo "== exact GitHub prerelease metadata is rejected =="
printf '{"tag_name":"v1.0.0","draft":false,"prerelease":true}\n' >"$FIXTURES/tag.json"
if run_case exact-prerelease ok v1.0.0; then
    FAIL=$((FAIL + 1)); note "installer should reject a GitHub prerelease"
else
    check "prerelease classification enforced" "grep -q 'marked as a prerelease' \"\$WORK/exact-prerelease/err.log\""
fi

echo "== exact endpoint must return the requested tag =="
printf '{"tag_name": "v9.9.9", "draft": false, "prerelease": false}\n' >"$FIXTURES/tag.json"
if run_case tag-mismatch ok v1.0.0; then
    FAIL=$((FAIL + 1)); note "installer should reject a mismatched API tag"
else
    check "tag mismatch explained" "grep -q \"expected 'v1.0.0'\" \"\$WORK/tag-mismatch/err.log\""
fi
printf '{"tag_name":"v1.0.0","draft":false,"prerelease":false}\n' >"$FIXTURES/tag.json"

echo "== latest chooses the highest stable semver =="
printf '[{"tag_name":"v1.2.0","draft":false,"prerelease":false},{"tag_name":"v2.0.0-rc.1","draft":false,"prerelease":true},{"draft":false,"prerelease":false,"tag_name":"v1.10.0"}]\n' >"$FIXTURES/latest.json"
make_fake_binary "$FIXTURES/$BINARY_NAME" "v1.10.0"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"
printf '{"object":{"sha":"%s","type":"commit"}}\n' "$COMMIT" >"$FIXTURES/ref.json"
if run_case highest none; then
    check "highest stable selected" "[[ \"\$(\"\$BIN\" --version | head -1)\" == 'hetki version v1.10.0' ]]"
else
    FAIL=$((FAIL + 1)); note "installer failed: $(tail -3 "$WORK/highest/err.log")"
fi
make_fake_binary "$FIXTURES/$BINARY_NAME" "v1.0.0"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"
printf '[{"tag_name":"v1.0.0","draft":false,"prerelease":false}]\n' >"$FIXTURES/latest.json"
printf '{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","object":{"sha":"%s","type":"commit"}}\n' "$COMMIT" >"$FIXTURES/ref.json"

echo "== replacement preserves the existing mode =="
if HETKI_TEST_PRESEED_MODE=6755 run_case preserve-mode ok latest v0.9.0; then
    mode="$(stat -c '%a' "$BIN" 2>/dev/null || stat -f '%Mp%Lp' "$BIN")"
    check "existing mode preserved" "[[ \"\$mode\" == 6755 ]]"
else
    FAIL=$((FAIL + 1)); note "installer failed: $(tail -3 "$WORK/preserve-mode/err.log")"
fi

echo "== source install pins sumdb, tag, and commit =="
if HETKI_FROM_SOURCE=1 run_case source ok; then
    check "source binary installed" "[[ -x \"\$BIN\" ]]"
    check "source authentication pinned" "grep -q 'GOPROXY=direct GOSUMDB=sum.golang.org GONOSUMDB= GOPRIVATE= GONOPROXY= GOMODCACHE=.* GOCACHE=.* GOTMPDIR=.' \"\$WORK/source/go.log\""
    check "source tag pinned" "grep -q 'github.com/MuhametSmaili/hetki@v1.0.0' \"\$WORK/source/go.log\""
else
    FAIL=$((FAIL + 1)); note "source installer failed: $(tail -3 "$WORK/source/err.log")"
fi

echo "== non-regular installation is refused =="
if HETKI_TEST_PRESEED_FIFO=1 run_case fifo ok latest ignored; then
    FAIL=$((FAIL + 1)); note "installer should reject a FIFO target"
else
    check "FIFO remains" "[[ -p \"\$BIN\" ]]"
    check "non-regular rejection explained" "grep -q 'non-regular installation' \"\$WORK/fifo/err.log\""
fi

echo "== unwritable destination preserves the existing installation =="
if [[ "$(id -u)" -eq 0 ]]; then
    note "skipped unwritable-directory probe as root"
else
    if HETKI_TEST_INSTALL_DIR_MODE=0555 run_case permission-denied ok latest v0.9.0; then
        chmod 0755 "$HETKI_INSTALL_DIR"
        FAIL=$((FAIL + 1)); note "installer should fail for an unwritable destination"
    else
        chmod 0755 "$HETKI_INSTALL_DIR"
        check "old binary survives permission failure" "[[ \"\$(\"\$BIN\" --version)\" == *'version v0.9.0'* ]]"
        check "permission failure leaves no recovery files" "[[ ! -e \"\$BIN.hetki-backup\" && ! -e \"\$BIN.hetki-update-lock\" ]]"
    fi
fi

echo "== symlinked installation is refused =="
if HETKI_TEST_PRESEED_SYMLINK=1 run_case symlink ok latest v0.9.0; then
    FAIL=$((FAIL + 1)); note "installer should reject a symlinked target"
else
    check "symlink remains" "[[ -L \"\$BIN\" ]]"
    check "symlink rejection explained" "grep -q 'symlinked installation' \"\$WORK/symlink/err.log\""
fi

echo "== binary reporting the wrong version rolls back over the old one =="
cp "$FIXTURES/hetki-liar-$OS-$ARCH" "$FIXTURES/$BINARY_NAME" # binary lies about version
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"
if run_case liar ok latest v0.9.0; then
    FAIL=$((FAIL + 1)); note "installer should have failed"
else
    check "old binary restored" "[[ \"\$(\"\$BIN\" --version)\" == *'version v0.9.0'* ]]"
    check "no backup left" "[[ ! -e \"\$BIN.hetki-backup\" ]]"
fi
# restore the good fixtures for any future runs
make_fake_binary "$FIXTURES/$BINARY_NAME" "v1.0.0"
printf '%s  %s\n' "$(sha "$FIXTURES/$BINARY_NAME")" "$BINARY_NAME" >"$FIXTURES/checksums.txt"

echo "== pre-rename cleanup preserves destination and removes owned backup =="
INSTALL_LIB="$WORK/install-lib.sh"
grep -v '^main "\$@"$' "$ROOT/install.sh" >"$INSTALL_LIB"
rollback_dir="$WORK/pre-rename"
mkdir -p "$rollback_dir"
printf old >"$rollback_dir/hetki"
ln "$rollback_dir/hetki" "$rollback_dir/hetki.hetki-backup"
if bash -c 'source "$1"; ROLLBACK_TARGET="$2/hetki"; ROLLBACK_BACKUP="$2/hetki.hetki-backup"; ROLLBACK_BACKUP_ID="$(file_id "$ROLLBACK_BACKUP")"; ORIGINAL_TARGET_ID="$(file_id "$ROLLBACK_TARGET")"; REPLACEMENT_PENDING=1; cleanup; [[ "$(cat "$2/hetki")" == old && ! -e "$2/hetki.hetki-backup" ]]' \
    _ "$INSTALL_LIB" "$rollback_dir"; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); note "pre-rename cleanup damaged the destination or left a backup"
fi

echo "== pre-rename cleanup preserves a concurrently changed destination =="
race_dir="$WORK/pre-rename-race"
mkdir -p "$race_dir"
printf old >"$race_dir/hetki"
ln "$race_dir/hetki" "$race_dir/hetki.hetki-backup"
original_id="$(stat -c '%d:%i' "$race_dir/hetki" 2>/dev/null || stat -f '%d:%i' "$race_dir/hetki")"
backup_id="$(stat -c '%d:%i' "$race_dir/hetki.hetki-backup" 2>/dev/null || stat -f '%d:%i' "$race_dir/hetki.hetki-backup")"
rm "$race_dir/hetki"; printf external >"$race_dir/hetki"
if bash -c 'source "$1"; ROLLBACK_TARGET="$2/hetki"; ROLLBACK_BACKUP="$2/hetki.hetki-backup"; ROLLBACK_BACKUP_ID="$3"; ORIGINAL_TARGET_ID="$4"; CANDIDATE_ID=unused; REPLACEMENT_PENDING=1; cleanup; REPLACEMENT_PENDING=0; [[ "$(cat "$2/hetki")" == external && "$(cat "$2/hetki.hetki-backup")" == old ]]' \
    _ "$INSTALL_LIB" "$race_dir" "$backup_id" "$original_id"; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); note "pre-rename cleanup overwrote a concurrent destination"
fi

echo "== fresh-install cleanup preserves a concurrently created destination =="
fresh_race_dir="$WORK/fresh-race"
mkdir -p "$fresh_race_dir"
printf candidate >"$fresh_race_dir/candidate"
candidate_id="$(stat -c '%d:%i' "$fresh_race_dir/candidate" 2>/dev/null || stat -f '%d:%i' "$fresh_race_dir/candidate")"
printf external >"$fresh_race_dir/hetki"
if bash -c 'source "$1"; ROLLBACK_TARGET="$2/hetki"; ROLLBACK_BACKUP=""; CANDIDATE_ID="$3"; REPLACEMENT_PENDING=1; cleanup; [[ "$(cat "$2/hetki")" == external ]]' \
    _ "$INSTALL_LIB" "$fresh_race_dir" "$candidate_id"; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); note "fresh-install cleanup removed a concurrent destination"
fi

echo "== TERM after replacement restores the previous binary =="
term_dir="$WORK/term-after-replace"
mkdir -p "$term_dir"
printf old >"$term_dir/old"
printf candidate >"$term_dir/hetki"
ln "$term_dir/old" "$term_dir/hetki.hetki-backup"
if bash -c 'source "$1"; ROLLBACK_TARGET="$2/hetki"; ROLLBACK_BACKUP="$2/hetki.hetki-backup"; ROLLBACK_BACKUP_ID="$(file_id "$ROLLBACK_BACKUP")"; CANDIDATE_ID="$(file_id "$ROLLBACK_TARGET")"; REPLACEMENT_PENDING=4; kill -TERM $$' \
    _ "$INSTALL_LIB" "$term_dir"; then
    FAIL=$((FAIL + 1)); note "TERM probe should exit non-zero"
else
    check "TERM restored previous bytes" "[[ \"\$(cat '$term_dir/hetki')\" == old ]]"
    check "TERM removed recovery backup" "[[ ! -e '$term_dir/hetki.hetki-backup' ]]"
fi

echo "== verified-state cleanup removes owned backup without rollback =="
verified_dir="$WORK/verified-state"
mkdir -p "$verified_dir"
printf new >"$verified_dir/hetki"
printf old >"$verified_dir/hetki.hetki-backup"
if bash -c 'source "$1"; ROLLBACK_TARGET="$2/hetki"; ROLLBACK_BACKUP="$2/hetki.hetki-backup"; ROLLBACK_BACKUP_ID="$(file_id "$ROLLBACK_BACKUP")"; REPLACEMENT_PENDING=2; cleanup; [[ "$(cat "$2/hetki")" == new && ! -e "$2/hetki.hetki-backup" ]]' \
    _ "$INSTALL_LIB" "$verified_dir"; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); note "verified-state cleanup changed destination or left a backup"
fi

echo "== terminating installer during curl kills the network process =="
curl_pid_file="$WORK/curl.pid"
PATH="$STUBS:$PATH" HETKI_TEST_CURL_PID_FILE="$curl_pid_file" \
    bash -c 'source "$1"; bounded_curl 1024 30 -fsSL https://example.invalid -o "$2"' \
    _ "$INSTALL_LIB" "$WORK/curl.out" >/dev/null 2>&1 &
installer_pid=$!
for _ in {1..50}; do [[ -s "$curl_pid_file" ]] && break; sleep 0.02; done
if [[ ! -s "$curl_pid_file" ]]; then
    FAIL=$((FAIL + 1)); note "curl termination probe did not start"
else
    curl_pid="$(cat "$curl_pid_file")"
    kill -TERM "$installer_pid" 2>/dev/null || true
    wait "$installer_pid" 2>/dev/null || true
    sleep 0.2
    if kill -0 "$curl_pid" 2>/dev/null; then
        kill -KILL "$curl_pid" 2>/dev/null || true
        FAIL=$((FAIL + 1)); note "curl survived installer termination"
    else
        PASS=$((PASS + 1))
    fi
fi

echo "== terminating installer kills subprocess descendants =="
child_pid_file="$WORK/descendant.pid"
bash -c 'source "$1"; run_with_timeout 30 sh -c '\''sleep 30 & echo $! > "$1"; wait'\'' _ "$2"' \
    _ "$INSTALL_LIB" "$child_pid_file" >/dev/null 2>&1 &
installer_pid=$!
for _ in {1..50}; do [[ -s "$child_pid_file" ]] && break; sleep 0.02; done
if [[ ! -s "$child_pid_file" ]]; then
    FAIL=$((FAIL + 1)); note "descendant probe did not start"
else
    child_pid="$(cat "$child_pid_file")"
    kill -TERM "$installer_pid" 2>/dev/null || true
    wait "$installer_pid" 2>/dev/null || true
    sleep 0.2
    if kill -0 "$child_pid" 2>/dev/null; then
        kill -KILL "$child_pid" 2>/dev/null || true
        FAIL=$((FAIL + 1)); note "installer descendant survived termination"
    else
        PASS=$((PASS + 1))
    fi
fi

report
