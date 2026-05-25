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

# ============================================================================
# Build Phase
# ============================================================================

install_go() {
    log_info "Installing Go $GO_VERSION..."
    
    if ! command_exists apk; then
        log_error "apk not found. This script requires Alpine Linux."
        exit 1
    fi
    
    # Try to install from apk repository
    log_info "Attempting to install Go from Alpine repository..."
    if apk add --no-cache go 2>/dev/null; then
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
    log_info "Installing build dependencies..."
    
    # Update package index first
    if ! apk update 2>/dev/null; then
        log_error "Failed to update package index"
        log_error "Please check your internet connection"
        exit 1
    fi
    
    # Install dependencies
    if ! apk add --no-cache \
        build-base \
        git \
        curl \
        wget \
        pkgconfig 2>/dev/null; then
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
    
    if [ -f "$alpine_js_file" ]; then
        local file_size
        file_size=$(wc -c < "$alpine_js_file")
        if [ "$file_size" -gt 1000 ]; then
            log_success "Alpine.js already downloaded ($file_size bytes)"
            return 0
        fi
    fi
    
    if command_exists curl; then
        curl -fsSL "$alpine_js_url" -o "$alpine_js_file"
    elif command_exists wget; then
        wget -q "$alpine_js_url" -O "$alpine_js_file"
    else
        log_error "curl or wget required to download Alpine.js"
        exit 1
    fi
    
    local file_size
    file_size=$(wc -c < "$alpine_js_file")
    if [ "$file_size" -gt 1000 ]; then
        log_success "Alpine.js downloaded ($file_size bytes)"
    else
        log_error "Alpine.js download failed or file is too small"
        exit 1
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
    
    export CGO_ENABLED=0
    
    log_info "Building webadmin..."
    if ! go build -o bin/webadmin ./cmd/webadmin 2>&1; then
        log_error "Failed to build webadmin"
        log_error "Check the error messages above"
        exit 1
    fi
    
    log_info "Building roothelper..."
    if ! go build -o bin/roothelper ./cmd/roothelper 2>&1; then
        log_error "Failed to build roothelper"
        log_error "Check the error messages above"
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
    log_info "Creating system user..."
    
    if id "$WEBADMIN_USER" >/dev/null 2>&1; then
        log_warn "User $WEBADMIN_USER already exists"
    else
        adduser -S -D -H -s /sbin/nologin "$WEBADMIN_USER"
        log_success "User $WEBADMIN_USER created"
    fi
}

create_directories() {
    log_info "Creating directories..."
    
    for dir in "$CONFIG_DIR" "$RUN_DIR" "$LOG_DIR"; do
        if [ ! -d "$dir" ]; then
            mkdir -p "$dir"
            log_success "Created $dir"
        fi
    done
    
    chmod 750 "$CONFIG_DIR"
    chmod 750 "$RUN_DIR"
    chmod 750 "$LOG_DIR"
    chown root:"$WEBADMIN_GROUP" "$CONFIG_DIR"
    chown root:"$WEBADMIN_GROUP" "$RUN_DIR"
    chown root:"$WEBADMIN_GROUP" "$LOG_DIR"
    
    log_success "Directories configured"
}

install_binaries() {
    log_info "Installing binaries..."
    
    install -Dm755 "$SCRIPT_DIR/bin/webadmin" "$INSTALL_PREFIX/webadmin"
    install -Dm755 "$SCRIPT_DIR/bin/roothelper" "$INSTALL_PREFIX/roothelper"
    
    log_success "Binaries installed to $INSTALL_PREFIX"
}

create_config() {
    log_info "Creating configuration..."
    
    if [ -f "$CONFIG_DIR/config.json" ]; then
        log_warn "Config already exists at $CONFIG_DIR/config.json"
        return 0
    fi
    
    cat > "$CONFIG_DIR/config.json" <<'EOF'
{
  "listen": ":8080",
  "ipc_socket": "/run/webadmin/ipc.sock",
  "session_ttl": 3600,
  "ws_max_conns": 10,
  "rate_limit_rps": 20,
  "allowed_helpers": ["/sbin/rc-service", "/sbin/rc-status", "/sbin/apk"]
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
    log_info "Default password: admin"
    log_warn "IMPORTANT: Change this in production!"
}

install_init_scripts() {
    log_info "Installing OpenRC init scripts..."
    
    cat > /etc/init.d/roothelper <<'INITEOF'
#!/sbin/openrc-run

description="Alpine WebAdmin Root Helper"
command="/usr/sbin/roothelper"
command_args="-config /etc/webadmin/config.json"
pidfile="/run/webadmin/roothelper.pid"
command_background=true

depend() {
    need localmount
    after firewall
}

start_pre() {
    checkpath --directory --mode 0750 --owner root:webadmin /run/webadmin
    checkpath --file --mode 0640 --owner root:webadmin /etc/webadmin/config.json
}
INITEOF
    
    chmod 755 /etc/init.d/roothelper
    
    cat > /etc/init.d/webadmin <<'INITEOF'
#!/sbin/openrc-run

description="Alpine WebAdmin Web Server"
command="/usr/sbin/webadmin"
command_args="-config /etc/webadmin/config.json"
command_user="webadmin:webadmin"
pidfile="/run/webadmin/webadmin.pid"
command_background=true

depend() {
    need net roothelper
}

start_pre() {
    checkpath --directory --mode 0750 --owner webadmin:webadmin /run/webadmin
}
INITEOF
    
    chmod 755 /etc/init.d/webadmin
    
    log_success "Init scripts installed"
}

enable_services() {
    log_info "Enabling services at boot..."
    
    rc-update add roothelper default
    rc-update add webadmin default
    
    log_success "Services enabled at boot"
}

start_services() {
    log_info "Starting services..."
    
    log_info "Starting roothelper..."
    rc-service roothelper start || log_warn "roothelper failed to start (may need manual intervention)"
    sleep 1
    
    log_info "Starting webadmin..."
    rc-service webadmin start || log_warn "webadmin failed to start (may need manual intervention)"
    sleep 1
    
    log_success "Services started"
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
    fi
    
    if [ $errors -eq 0 ]; then
        log_success "Deployment verified"
        return 0
    else
        log_error "Deployment verification failed with $errors error(s)"
        return 1
    fi
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
  all         Build and deploy (requires root for deploy phase)
  help        Show this help message

Examples:
  # Build on development machine
  ./setup.sh build

  # Deploy to testbench (requires pre-built binaries)
  sudo ./setup.sh deploy

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
            create_password_hash
            install_init_scripts
            enable_services
            start_services
            verify_deployment
            
            log_info ""
            log_success "Deployment complete!"
            log_info ""
            log_info "Access the web interface:"
            log_info "  URL: http://localhost:8080"
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
            create_password_hash
            install_init_scripts
            enable_services
            start_services
            verify_deployment
            
            log_info ""
            log_success "Setup complete!"
            log_info ""
            log_info "Access the web interface:"
            log_info "  URL: http://localhost:8080"
            log_info "  Username: admin"
            log_info "  Password: admin"
            log_info ""
            ;;
            
        help|--help|-h)
            print_usage
            ;;
            
        *)
            log_error "Unknown command: $COMMAND"
            print_usage
            exit 1
            ;;
    esac
}

main
