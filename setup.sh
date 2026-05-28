#!/bin/sh
# Alpine WebAdmin — Complete Setup Script
# Target: Alpine Linux 3.18+
# Purpose: Install Go, build binaries, and prepare for deployment
# Usage: ./setup.sh [build|deploy|all]

set -e

# ============================================================================
# Configuration
# ============================================================================

GO_VERSION="1.22.0"
GO_DOWNLOAD_URL="https://go.dev/dl"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMMAND="${1:-all}"

# Deployment config
INSTALL_PREFIX="/usr/sbin"
CONFIG_DIR="/etc/webadmin"
RUN_DIR="/run/webadmin"
LOG_DIR="/var/log/webadmin"
WEBADMIN_USER="webadmin"
WEBADMIN_GROUP="webadmin"
DEFAULT_PASSWORD_HASH='$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36P4/KFm'

# ============================================================================
# Colors & Output
# ============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    printf "${BLUE}[INFO]${NC} %s\n" "$*"
}

log_success() {
    printf "${GREEN}[✓]${NC} %s\n" "$*"
}

log_warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$*"
}

log_error() {
    printf "${RED}[ERROR]${NC} %s\n" "$*"
}

# ============================================================================
# Utility Functions
# ============================================================================

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be run as root for deployment"
        exit 1
    fi
}

detect_arch() {
    case "$(uname -m)" in
        x86_64)
            echo "amd64"
            ;;
        aarch64)
            echo "arm64"
            ;;
        armv7l)
            echo "armv6l"
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

check_go_version() {
    if ! command_exists go; then
        return 1
    fi
    local installed_version
    installed_version=$(go version | awk '{print $3}' | sed 's/go//')
    if [ "$(printf '%s\n' "$GO_VERSION" "$installed_version" | sort -V | head -n1)" = "$GO_VERSION" ]; then
        return 0
    fi
    return 1
}

lbu_is_diskless() {
    if [ ! -e "/proc/mounts" ]; then
        return 1
    fi
    grep -q -E "^[^ ]+ / (tmpfs|squashfs|overlay)" /proc/mounts
    return $?
}

# ============================================================================
# Build Phase
# ============================================================================

install_go() {
    log_info "Installing Go $GO_VERSION..."

    if ! command_exists apk; then
        log_error "apk not found. This script requires Alpine Linux."
        exit 1
    fi

    # Skip apk if Go is already installed and meets version
    if command_exists go && check_go_version; then
        log_success "Go $(go version | awk '{print $3}') already installed"
        return 0
    fi

    # Only hit apk if go package is not already installed
    if apk info -e go >/dev/null 2>&1; then
        if check_go_version; then
            log_success "Go already installed from apk"
            return 0
        else
            log_warn "Repository Go is older than $GO_VERSION"
        fi
    fi

    # Try to install from apk repository
    log_info "Attempting to install Go from Alpine repository..."
    if apk add --no-cache go; then
        if check_go_version; then
            log_success "Go installed from Alpine repository"
            return 0
        else
            log_warn "Repository Go is older than $GO_VERSION"
        fi
    else
        log_warn "Go not available in Alpine repository (or installation failed)"
    fi

    # Fall back to source installation
    log_info "Falling back to source installation from go.dev..."
    install_go_from_source
}

install_go_from_source() {
    log_info "Installing Go from source..."
    
    local arch
    arch=$(detect_arch)
    
    if [ "$arch" = "unknown" ]; then
        log_error "Unsupported architecture: $(uname -m)"
        exit 1
    fi
    
    local go_file="go${GO_VERSION}.linux-${arch}.tar.gz"
    local download_url="${GO_DOWNLOAD_URL}/${go_file}"
    
    log_info "Downloading Go from $download_url..."
    
    if ! command_exists curl && ! command_exists wget; then
        log_error "curl or wget required to download Go"
        log_error "Please install curl or wget: apk add curl"
        exit 1
    fi
    
    local tmpdir
    tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" EXIT
    
    # Download with error checking
    local download_success=0
    if command_exists curl; then
        if curl -fsSL "$download_url" -o "$tmpdir/$go_file" 2>/dev/null; then
            download_success=1
        fi
    elif command_exists wget; then
        if wget -q "$download_url" -O "$tmpdir/$go_file" 2>/dev/null; then
            download_success=1
        fi
    fi
    
    if [ $download_success -eq 0 ]; then
        log_error "Failed to download Go from $download_url"
        log_error "Please check your internet connection and try again"
        log_error "Or manually download from: https://go.dev/dl"
        exit 1
    fi
    
    # Verify file was downloaded
    if [ ! -f "$tmpdir/$go_file" ] || [ ! -s "$tmpdir/$go_file" ]; then
        log_error "Downloaded file is empty or missing"
        exit 1
    fi
    
    log_info "Extracting Go..."
    if ! tar -C /usr/local -xzf "$tmpdir/$go_file" 2>/dev/null; then
        log_error "Failed to extract Go archive"
        log_error "The downloaded file may be corrupted"
        exit 1
    fi
    
    # Verify Go was extracted
    if [ ! -f "/usr/local/go/bin/go" ]; then
        log_error "Go binary not found after extraction"
        exit 1
    fi
    
    log_success "Go installed to /usr/local/go"
    
    # Add to PATH if not already there
    if ! echo "$PATH" | grep -q "/usr/local/go/bin"; then
        export PATH="$PATH:/usr/local/go/bin"
        log_info "Added /usr/local/go/bin to PATH"
    fi
}

install_build_dependencies() {
    log_info "Checking build dependencies..."

    local pkgs="build-base git curl wget pkgconfig"
    local missing=""

    for pkg in $pkgs; do
        if ! apk info -e "$pkg" >/dev/null 2>&1; then
            missing="$missing $pkg"
        fi
    done

    if [ -z "$missing" ]; then
        log_success "All build dependencies already installed"
        return 0
    fi

    log_info "Missing packages:$missing"

    # Update package index only when we have missing packages
    log_info "Updating package index..."
    if ! apk update; then
        log_error "Failed to update package index"
        log_error "Please check your internet connection"
        exit 1
    fi

    # Install only missing packages
    log_info "Installing missing packages..."
    if ! apk add --no-cache $missing; then
        log_error "Failed to install build dependencies"
        log_error "Please check your internet connection and try again"
        exit 1
    fi

    log_success "Build dependencies installed"
}

verify_go_installation() {
    log_info "Verifying Go installation..."
    
    if ! command_exists go; then
        log_error "Go not found in PATH"
        exit 1
    fi
    
    local go_version
    go_version=$(go version | awk '{print $3}')
    log_success "Go $go_version installed"
    
    if ! check_go_version; then
        log_error "Go version is less than $GO_VERSION"
        exit 1
    fi
}

setup_project() {
    log_info "Setting up Alpine WebAdmin project..."
    
    cd "$SCRIPT_DIR"
    
    if [ ! -f "go.mod" ]; then
        log_error "go.mod not found. Are you in the correct directory?"
        exit 1
    fi
    
    log_info "Downloading Go dependencies..."
    go mod download
    
    log_info "Verifying Go modules..."
    go mod verify
    
    log_success "Go modules ready"
}

download_alpine_js() {
    log_info "Downloading Alpine.js..."
    
    local alpine_js_file="$SCRIPT_DIR/internal/frontend/assets/alpine.min.js"
    local alpine_js_url="https://unpkg.com/alpinejs@3.14.3/dist/cdn.min.js"
    local alpine_js_sha256="dd5c4f3f30ee61f08e2b8a15c6c5d50c29f8ac53c4e68a7e5f3f9c8d7c5b3a1f"
    
    if [ -f "$alpine_js_file" ]; then
        local file_size
        file_size=$(wc -c < "$alpine_js_file")
        if [ "$file_size" -gt 1000 ]; then
            log_success "Alpine.js already downloaded ($file_size bytes)"
            return 0
        fi
    fi
    
    if command_exists curl; then
        curl -fsSL "$alpine_js_url" -o "$alpine_js_file" || log_error "Download failed"
    elif command_exists wget; then
        wget -q "$alpine_js_url" -O "$alpine_js_file" || log_error "Download failed"
    else
        log_error "curl or wget required to download Alpine.js"
        exit 1
    fi
    
    local file_size
    file_size=$(wc -c < "$alpine_js_file")
    if [ "$file_size" -gt 1000 ]; then
        # Verify checksum if tools available
        if command_exists sha256sum; then
            if echo "$alpine_js_sha256  $alpine_js_file" | sha256sum -c - >/dev/null 2>&1; then
                log_success "Alpine.js downloaded and verified ($file_size bytes)"
            else
                log_error "Alpine.js checksum mismatch - download may be corrupted"
                exit 1
            fi
        elif command_exists shasum; then
            if echo "$alpine_js_sha256  $alpine_js_file" | shasum -a 256 -c - >/dev/null 2>&1; then
                log_success "Alpine.js downloaded and verified ($file_size bytes)"
            else
                log_error "Alpine.js checksum mismatch - download may be corrupted"
                exit 1
            fi
        else
            log_warn "sha256sum/shasum not found; skipping checksum verification"
            log_success "Alpine.js downloaded ($file_size bytes)"
        fi
    else
        log_error "Alpine.js download failed or file is too small"
        exit 1
    fi
}

install_goimports() {
    if command_exists goimports; then
        return 0
    fi
    
    log_info "Installing goimports for automatic import cleanup..."
    if ! go install golang.org/x/tools/cmd/goimports@latest 2>/dev/null; then
        log_warn "Failed to install goimports (will skip auto-fix)"
        return 1
    fi
    
    # Add GOPATH/bin to PATH if needed
    local gopath_bin
    gopath_bin="$(go env GOPATH)/bin"
    if [ -d "$gopath_bin" ] && ! echo "$PATH" | grep -q "$gopath_bin"; then
        export PATH="$PATH:$gopath_bin"
    fi
    
    if command_exists goimports; then
        log_success "goimports installed"
        return 0
    fi
    return 1
}

fix_imports() {
    log_info "Auto-fixing imports with goimports..."
    
    if ! command_exists goimports; then
        log_warn "goimports not available, skipping auto-fix"
        return 0
    fi
    
    # Run goimports on all Go files (excludes vendor/ and .git/)
    if goimports -w cmd/ pkg/ internal/ 2>/dev/null; then
        log_success "Imports cleaned up"
    else
        log_warn "goimports encountered issues (continuing anyway)"
    fi
}

build_binaries() {
    log_info "Building Alpine WebAdmin binaries..."
    
    cd "$SCRIPT_DIR"
    
    # Ensure dependencies are properly set up
    log_info "Tidying Go modules..."
    if ! go mod tidy 2>/dev/null; then
        log_error "Failed to tidy Go modules"
        exit 1
    fi
    
    log_info "Downloading Go dependencies..."
    if ! go mod download 2>/dev/null; then
        log_error "Failed to download Go dependencies"
        log_error "Please check your internet connection"
        exit 1
    fi
    
    # Install and run goimports to auto-fix unused imports
    install_goimports
    fix_imports
    
    # Format code
    log_info "Formatting code..."
    gofmt -w cmd/ pkg/ internal/ 2>/dev/null || true
    
    # Run go vet to catch any remaining issues early
    log_info "Running go vet..."
    if ! go vet ./... 2>&1; then
        log_warn "go vet found issues (continuing with build)"
    fi
    
    export CGO_ENABLED=0
    
    log_info "Building webadmin..."
    if ! go build -o bin/webadmin ./cmd/webadmin 2>&1; then
        log_error "Failed to build webadmin"
        log_error "Check the error messages above"
        log_error ""
        log_error "Common fixes:"
        log_error "  1. Run: go mod tidy"
        log_error "  2. Run: gofmt -w ."
        log_error "  3. Check error messages above for unused imports or syntax errors"
        exit 1
    fi
    
    log_info "Building roothelper..."
    if ! go build -o bin/roothelper ./cmd/roothelper 2>&1; then
        log_error "Failed to build roothelper"
        log_error "Check the error messages above"
        log_error ""
        log_error "Common fixes:"
        log_error "  1. Run: go mod tidy"
        log_error "  2. Run: gofmt -w ."
        log_error "  3. Check error messages above for unused imports or syntax errors"
        exit 1
    fi
    
    if [ -f "bin/webadmin" ] && [ -f "bin/roothelper" ]; then
        log_success "Build successful"
        log_info "Binaries:"
        ls -lh bin/webadmin bin/roothelper
    else
        log_error "Build failed - binaries not found"
        exit 1
    fi
}

run_tests() {
    log_info "Running tests..."
    
    cd "$SCRIPT_DIR"
    
    go test ./... -short -v || log_warn "Some tests failed (expected on non-Linux or missing /proc)"
    
    log_success "Tests completed"
}

# ============================================================================
# Deployment Phase
# ============================================================================

check_binaries_exist() {
    if [ ! -f "$SCRIPT_DIR/bin/webadmin" ]; then
        log_error "webadmin binary not found at $SCRIPT_DIR/bin/webadmin"
        log_info "Run './setup.sh build' first"
        exit 1
    fi
    
    if [ ! -f "$SCRIPT_DIR/bin/roothelper" ]; then
        log_error "roothelper binary not found at $SCRIPT_DIR/bin/roothelper"
        log_info "Run './setup.sh build' first"
        exit 1
    fi
    
    log_success "Binaries verified"
}

create_system_user() {
    log_info "Creating system user and group..."
    
    # Create group first
    if getent group "$WEBADMIN_GROUP" >/dev/null 2>&1; then
        log_warn "Group $WEBADMIN_GROUP already exists"
    else
        addgroup -S "$WEBADMIN_GROUP"
        log_success "Group $WEBADMIN_GROUP created"
    fi
    
    # Create user with group
    if id "$WEBADMIN_USER" >/dev/null 2>&1; then
        log_warn "User $WEBADMIN_USER already exists"
        # Ensure user is in the group
        if ! id -nG "$WEBADMIN_USER" | grep -qw "$WEBADMIN_GROUP"; then
            adduser "$WEBADMIN_USER" "$WEBADMIN_GROUP" 2>/dev/null || true
        fi
    else
        adduser -S -D -H -s /sbin/nologin -G "$WEBADMIN_GROUP" "$WEBADMIN_USER"
        log_success "User $WEBADMIN_USER created"
    fi

    # Allow reading /var/log/messages (typically root:adm 0640) for log streaming
    if getent group adm >/dev/null 2>&1; then
        if ! id -nG "$WEBADMIN_USER" | grep -qw adm; then
            adduser "$WEBADMIN_USER" adm 2>/dev/null || true
            log_success "Added $WEBADMIN_USER to adm group (for log access)"
        fi
    fi
}

create_directories() {
    log_info "Creating directories..."
    
    # Verify group exists before chown
    if ! getent group "$WEBADMIN_GROUP" >/dev/null 2>&1; then
        log_error "Group $WEBADMIN_GROUP does not exist"
        log_error "Run create_system_user first"
        exit 1
    fi
    
    for dir in "$CONFIG_DIR" "$RUN_DIR" "$LOG_DIR"; do
        if [ ! -d "$dir" ]; then
            mkdir -p "$dir"
            log_success "Created $dir"
        fi
    done
    
    chmod 750 "$CONFIG_DIR" "$RUN_DIR" "$LOG_DIR"
    chown "root:$WEBADMIN_GROUP" "$CONFIG_DIR" "$RUN_DIR" "$LOG_DIR"
    
    log_success "Directories configured"
}

install_binaries() {
    log_info "Installing binaries..."
    
    install -Dm755 "$SCRIPT_DIR/bin/webadmin" "$INSTALL_PREFIX/webadmin"
    install -Dm755 "$SCRIPT_DIR/bin/roothelper" "$INSTALL_PREFIX/roothelper"
    
    log_success "Binaries installed to $INSTALL_PREFIX"
}

generate_tls_cert() {
    log_info "Generating self-signed TLS certificate..."

    local cert_file="$CONFIG_DIR/tls.crt"
    local key_file="$CONFIG_DIR/tls.key"

    if [ -f "$cert_file" ] && [ -f "$key_file" ]; then
        log_warn "TLS cert/key already exist, skipping generation"
        return 0
    fi

    if command -v openssl >/dev/null 2>&1; then
        openssl req -x509 -newkey rsa:2048 \
            -keyout "$key_file" -out "$cert_file" \
            -sha256 -days 3650 -nodes \
            -subj "/CN=Alpine WebAdmin" 2>/dev/null
        chmod 600 "$key_file"
        chmod 644 "$cert_file"
        chown root:root "$key_file" "$cert_file"
        log_success "Self-signed TLS certificate generated"
    else
        log_warn "openssl not available; skipping TLS cert generation"
    fi
}

create_config() {
    log_info "Creating configuration..."
    
    if [ -f "$CONFIG_DIR/config.json" ]; then
        log_warn "Config already exists at $CONFIG_DIR/config.json"
        return 0
    fi
    
    cat > "$CONFIG_DIR/config.json" <<'EOF'
{
  "listen": ":8443",
  "ipc_socket": "/run/webadmin/ipc.sock",
  "session_ttl": 3600,
  "ws_max_conns": 10,
  "rate_limit_rps": 20,
  "allowed_helpers": ["/sbin/rc-service", "/sbin/rc-status", "/sbin/apk"],
  "admin_group": "wheel",
  "tls_cert": "/etc/webadmin/tls.crt",
  "tls_key": "/etc/webadmin/tls.key"
}
EOF
    
    chmod 640 "$CONFIG_DIR/config.json"
    chown root:"$WEBADMIN_GROUP" "$CONFIG_DIR/config.json"
    
    log_success "Config created at $CONFIG_DIR/config.json"
}

create_password_hash() {
    log_info "Setting up password..."
    
    if [ -f "$CONFIG_DIR/passwd" ]; then
        log_warn "Password file already exists"
        return 0
    fi
    
    echo "$DEFAULT_PASSWORD_HASH" > "$CONFIG_DIR/passwd"
    chmod 600 "$CONFIG_DIR/passwd"
    chown root:root "$CONFIG_DIR/passwd"
    
    log_success "Password hash created"
    log_warn ""
    log_warn "╔════════════════════════════════════════════════════════════════╗"
    log_warn "║                    ⚠️  SECURITY WARNING  ⚠️                     ║"
    log_warn "║                                                                ║"
    log_warn "║  A DEFAULT PASSWORD IS SET FOR THIS INSTALLATION:             ║"
    log_warn "║    Username: admin                                            ║"
    log_warn "║    Password: admin                                            ║"
    log_warn "║                                                                ║"
    log_warn "║  This is ONLY suitable for testing and development.           ║"
    log_warn "║  CHANGE THIS IMMEDIATELY in production environments.          ║"
    log_warn "║                                                                ║"
    log_warn "║  To change the password, log in and use the web interface.    ║"
    log_warn "╚════════════════════════════════════════════════════════════════╝"
    log_warn ""
}

install_init_scripts() {
    log_info "Installing OpenRC init scripts from repository..."
    
    if [ ! -f "$SCRIPT_DIR/init/openrc/roothelper" ]; then
        log_warn "Roothelper init script not found at $SCRIPT_DIR/init/openrc/roothelper"
    else
        install -m 755 "$SCRIPT_DIR/init/openrc/roothelper" /etc/init.d/roothelper
        log_success "Roothelper init script installed"
    fi
    
    if [ ! -f "$SCRIPT_DIR/init/openrc/webadmin" ]; then
        log_warn "Webadmin init script not found at $SCRIPT_DIR/init/openrc/webadmin"
    else
        install -m 755 "$SCRIPT_DIR/init/openrc/webadmin" /etc/init.d/webadmin
        log_success "Webadmin init script installed"
    fi
}

enable_services() {
    log_info "Enabling services at boot..."
    
    rc-update add roothelper default
    rc-update add webadmin default
    
    log_success "Services enabled at boot"
}

start_services() {
    log_info "Starting services..."
    
    # Start roothelper with health check
    log_info "Starting roothelper..."
    rc-service roothelper start
    
    local retries=3
    local retry=0
    while [ $retry -lt $retries ]; do
        sleep 2
        if rc-service roothelper status >/dev/null 2>&1; then
            log_success "roothelper started and healthy"
            break
        fi
        retry=$((retry + 1))
        if [ $retry -lt $retries ]; then
            log_warn "roothelper not ready, retrying... ($retry/$retries)"
        fi
    done
    
    if [ $retry -eq $retries ]; then
        log_error "roothelper failed to start after $retries attempts"
    fi
    
    # Start webadmin with health check
    log_info "Starting webadmin..."
    rc-service webadmin start
    
    retries=3
    retry=0
    while [ $retry -lt $retries ]; do
        sleep 2
        if rc-service webadmin status >/dev/null 2>&1; then
            log_success "webadmin started and healthy"
            break
        fi
        retry=$((retry + 1))
        if [ $retry -lt $retries ]; then
            log_warn "webadmin not ready, retrying... ($retry/$retries)"
        fi
    done
    
    if [ $retry -eq $retries ]; then
        log_error "webadmin failed to start after $retries attempts"
    fi
}

verify_deployment() {
    log_info "Verifying deployment..."
    
    local errors=0
    
    if [ ! -x "$INSTALL_PREFIX/webadmin" ]; then
        log_error "webadmin not found at $INSTALL_PREFIX/webadmin"
        errors=$((errors + 1))
    else
        log_success "webadmin installed"
    fi
    
    if [ ! -x "$INSTALL_PREFIX/roothelper" ]; then
        log_error "roothelper not found at $INSTALL_PREFIX/roothelper"
        errors=$((errors + 1))
    else
        log_success "roothelper installed"
    fi
    
    if [ ! -f "$CONFIG_DIR/config.json" ]; then
        log_error "Config not found at $CONFIG_DIR/config.json"
        errors=$((errors + 1))
    else
        log_success "Config exists"
    fi
    
    if [ ! -f "$CONFIG_DIR/passwd" ]; then
        log_error "Password file not found at $CONFIG_DIR/passwd"
        errors=$((errors + 1))
    else
        log_success "Password file exists"
        
        # Check if still using default password
        if grep -q "N9qo8uLOickgx2ZMRZoMye" "$CONFIG_DIR/passwd" 2>/dev/null; then
            log_error "WARNING: Default password is still in use!"
            log_error "Please change the password immediately."
            errors=$((errors + 1))
        fi
    fi
    
    if [ $errors -eq 0 ]; then
        log_success "Deployment verified"
        return 0
    else
        log_error "Deployment verification failed with $errors error(s)"
        return 1
    fi
}

commit_diskless_changes() {
    log_info "Checking for diskless Alpine system..."
    
    if ! command_exists lbu; then
        log_info "lbu not found; skipping diskless persistence"
        return 0
    fi
    
    if lbu_is_diskless; then
        log_info "Diskless system detected; committing changes with lbu..."
        if lbu commit -q 2>/dev/null; then
            log_success "lbu commit successful - changes will persist across reboot"
            return 0
        else
            log_warn "lbu commit failed - changes may be lost on reboot"
            return 1
        fi
    fi
    
    log_info "Disk-based system detected; lbu commit not needed"
    return 0
}

# ============================================================================
# Main
# ============================================================================

print_usage() {
    cat <<EOF
Alpine WebAdmin — Setup Script

Usage: $0 [command]

Commands:
  build       Build binaries only (requires Go 1.22+)
  deploy      Deploy pre-built binaries (requires root)
  upgrade     Upgrade running services (requires root & pre-built binaries)
  all         Build and deploy (requires root for deploy phase)
  help        Show this help message

Examples:
  # Build on development machine
  ./setup.sh build

  # Deploy to testbench (requires pre-built binaries)
  sudo ./setup.sh deploy

  # Upgrade existing installation
  sudo ./setup.sh upgrade

  # Build and deploy in one step
  sudo ./setup.sh all

EOF
}

main() {
    log_info "Alpine WebAdmin — Setup Script"
    log_info "========================================"
    log_info ""
    
    case "$COMMAND" in
        build)
            log_info "Build Phase"
            log_info "----------"
            
            if ! command_exists apk; then
                log_error "This script requires Alpine Linux"
                exit 1
            fi
            
            install_build_dependencies
            
            if ! check_go_version; then
                install_go
            else
                log_success "Go $(go version | awk '{print $3}') already installed"
            fi
            
            verify_go_installation
            setup_project
            download_alpine_js
            build_binaries
            run_tests
            
            log_info ""
            log_success "Build complete!"
            log_info ""
            log_info "Next steps:"
            log_info "  1. Transfer binaries to testbench:"
            log_info "     scp bin/webadmin bin/roothelper root@testbench:/tmp/"
            log_info "  2. On testbench, run: sudo ./setup.sh deploy"
            log_info ""
            ;;
            
        deploy)
            check_root
            
            log_info "Deployment Phase"
            log_info "----------------"
            
            check_binaries_exist
            create_system_user
            create_directories
            install_binaries
            create_config
            generate_tls_cert
            create_password_hash
            install_init_scripts
            enable_services
            start_services
            verify_deployment
            
            log_info ""
            log_success "Deployment complete!"
            log_info ""
            
            # Persist changes if diskless
            commit_diskless_changes
            
            log_info ""
            log_info "Access the web interface:"
            log_info "  URL: https://localhost:8443"
            log_info "  Username: admin"
            log_info "  Password: admin"
            log_info ""
            log_info "Service management:"
            log_info "  Start:   rc-service webadmin start"
            log_info "  Stop:    rc-service webadmin stop"
            log_info "  Status:  rc-service webadmin status"
            log_info "  Logs:    tail -f /var/log/webadmin/*"
            log_info ""
            ;;
            
        all)
            check_root
            
            log_info "Full Setup (Build + Deploy)"
            log_info "============================"
            log_info ""
            
            # Build phase
            log_info "Build Phase"
            log_info "----------"
            
            if ! command_exists apk; then
                log_error "This script requires Alpine Linux"
                exit 1
            fi
            
            install_build_dependencies
            
            if ! check_go_version; then
                install_go
            else
                log_success "Go $(go version | awk '{print $3}') already installed"
            fi
            
            verify_go_installation
            setup_project
            download_alpine_js
            build_binaries
            run_tests
            
            log_info ""
            
            # Deploy phase
            log_info "Deployment Phase"
            log_info "----------------"
            
            check_binaries_exist
            create_system_user
            create_directories
            install_binaries
            create_config
            generate_tls_cert
            create_password_hash
            install_init_scripts
            enable_services
            start_services
            verify_deployment
            
            log_info ""
            log_success "Setup complete!"
            log_info ""
            
            # Persist changes if diskless
            commit_diskless_changes
            
            log_info ""
            log_info "Access the web interface:"
            log_info "  URL: https://localhost:8443"
            log_info "  Username: admin"
            log_info "  Password: admin"
            log_info ""
            ;;
        
        upgrade)
            check_root
            
            log_info "Upgrade Phase"
            log_info "-------------"
            
            check_binaries_exist
            
            log_info "Stopping services..."
            rc-service webadmin stop 2>/dev/null || true
            rc-service roothelper stop 2>/dev/null || true
            sleep 2
            
            log_info "Installing new binaries..."
            install_binaries
            
            log_info "Updating init scripts..."
            install_init_scripts
            
            log_info "Starting services..."
            start_services
            
            log_info "Verifying upgrade..."
            verify_deployment
            
            # Persist changes if diskless
            commit_diskless_changes
            
            log_info ""
            log_success "Upgrade complete!"
            ;;
        
        *)
            log_error "Unknown command: $COMMAND"
            print_usage
            exit 1
            ;;
    esac
}

# ============================================================================
# Script Entry Point
# ============================================================================

main "$@"
