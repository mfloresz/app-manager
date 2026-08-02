#!/bin/sh
# install.sh — Instalación inicial de AP Manager
# 
# Uso:
#   curl -fsSL https://raw.githubusercontent.com/mfloresz/app-manager/main/install.sh | sh
#   # O descarga y ejecuta localmente:
#   chmod +x install.sh && ./install.sh
#
# Soporta: Linux (amd64, arm64, armv7) y Android/Termux (arm64, armv7)
# Para Termux: ejecuta dentro del entorno Termux.

set -e

# ═══════════════════════════════════════
# UTILITIES
# ═══════════════════════════════════════
info()  { printf "\033[1m%s\033[0m\n" "$*"; }
ok()    { printf "\033[32m✓\033[0m %s\n" "$*"; }
warn()  { printf "\033[33m⚠\033[0m %s\n" "$*"; }
err()   { printf "\033[31m✗\033[0m %s\n" "$*"; exit 1; }
ask()   { printf "\033[36m?\033[0m %s " "$*"; }
read_input() {
    if [ -t 0 ]; then
        read -r "$@"
    elif [ -r /dev/tty ]; then
        read -r "$@" < /dev/tty
    else
        return 1
    fi
}

# ═══════════════════════════════════════
# PLATFORM DETECTION
# ═══════════════════════════════════════

detect_platform() {
    OS=""
    ARCH=""
    SUFFIX=""

    # Detect Termux (Android)
    if [ -n "$PREFIX" ] && [ -d "$PREFIX/bin" ]; then
        OS="android"
        case "$(uname -m)" in
            aarch64) ARCH="arm64"; SUFFIX="android-arm64" ;;
            armv7l|arm)
                warn "Arquitectura detectada: $(uname -m)"
                ask "Instalar para armv7 (1) o arm64 (2)? [1/2]"
                choice=""
                read_input choice || true
                case "$choice" in
                    2|arm64) ARCH="arm64"; SUFFIX="android-arm64" ;;
                    *)       ARCH="armv7"; SUFFIX="android-armv7" ;;
                esac
                ;;
            *)
                err "Arquitectura no soportada en Termux: $(uname -m). Soporta: aarch64, armv7l, arm"
                ;;
        esac
        return
    fi

    # Detect standard Linux
    case "$(uname -s)" in
        Linux)  OS="linux" ;;
        Darwin)
            OS="darwin"
            case "$(uname -m)" in
                x86_64)  ARCH="amd64"; SUFFIX="darwin-amd64" ;;
                arm64)   ARCH="arm64"; SUFFIX="darwin-arm64" ;;
                *)       err "Arquitectura macOS no soportada: $(uname -m)" ;;
            esac
            return
            ;;
        *)
            err "Sistema operativo no soportado: $(uname -s). Solo Linux y Termux."
            ;;
    esac

    # Linux architecture
    case "$(uname -m)" in
        x86_64)
            ARCH="amd64"; SUFFIX="linux-amd64"
            ;;
        aarch64)
            ARCH="arm64"; SUFFIX="linux-arm64"
            ;;
        armv7l|arm)
            ARCH="armv7"; SUFFIX="linux-armv7"
            # Check if the user might want arm64 instead
            if [ "$(uname -m)" = "armv7l" ]; then
                warn "Detectado: armv7l (32 bits)"
                ask "¿Quieres instalar para armv7 (1) o arm64 (2)? [1/2]"
                choice=""
                read_input choice || true
                case "$choice" in
                    2|arm64) ARCH="arm64"; SUFFIX="linux-arm64" ;;
                esac
            fi
            ;;
        *)
            err "Arquitectura Linux no soportada: $(uname -m). Soporta: x86_64, aarch64, armv7l"
            ;;
    esac
}

# ═══════════════════════════════════════
# INSTALLATION
# ═══════════════════════════════════════

main() {
    echo ""
    info "╔══════════════════════════════════╗"
    info "║     AP Manager - Installer       ║"
    info "╚══════════════════════════════════╝"
    echo ""

    detect_platform
    ok "Plataforma detectada: ${OS}/${ARCH} (${SUFFIX})"

    # Check dependencies
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        err "Se requiere curl o wget para la descarga."
    fi

    # Determine installation directory
    if [ "${OS}" = "android" ]; then
        # Termux — use $PREFIX/bin
        DEST="${PREFIX}/bin"
        mkdir -p "$DEST"
        ok "Directorio de instalación: $DEST (Termux)"
    else
        # Linux — try several locations
        for dir in "${HOME}/.local/bin" "${HOME}/bin" "/usr/local/bin"; do
            mkdir -p "$dir" 2>/dev/null && DEST="$dir" && break
        done
        if [ -z "$DEST" ]; then
            err "No se pudo crear directorio de instalación."
        fi
        ok "Directorio de instalación: $DEST"
    fi

    # Fetch latest release info
    info ""
    info "Obteniendo última versión desde GitHub..."
    API_URL="https://api.github.com/repos/mfloresz/app-manager/releases/latest"

    if command -v curl >/dev/null 2>&1; then
        RELEASE_JSON=$(curl -sL "$API_URL" 2>/dev/null)
    else
        RELEASE_JSON=$(wget -qO- "$API_URL" 2>/dev/null)
    fi

    TAG=$(echo "$RELEASE_JSON" | grep '"tag_name"' | cut -d'"' -f4)
    if [ -z "$TAG" ]; then
        err "No se pudo obtener la última versión. Verifica tu conexión a GitHub."
    fi
    ok "Última versión: $TAG"

    # Build download URL — CI builds use: ap-manager-{suffix}-{tag}
    BINARY_NAME="ap-manager-${SUFFIX}-${TAG}"
    DOWNLOAD_URL="https://github.com/mfloresz/app-manager/releases/download/${TAG}/${BINARY_NAME}"

    info ""
    info "Descargando ${BINARY_NAME}..."
    info "  → ${DOWNLOAD_URL}"

    TMP_FILE=$(mktemp)
    if command -v curl >/dev/null 2>&1; then
        curl -fsL -o "$TMP_FILE" "$DOWNLOAD_URL" || err "Error al descargar (curl)."
    else
        wget -qO "$TMP_FILE" "$DOWNLOAD_URL" || err "Error al descargar (wget)."
    fi

    if [ ! -s "$TMP_FILE" ]; then
        rm -f "$TMP_FILE"
        err "Archivo descargado vacío. Verifica la URL o la plataforma."
    fi
    ok "Descarga completada ($(ls -lh "$TMP_FILE" | awk '{print $5}'))"

    # Install
    chmod 755 "$TMP_FILE"
    mv "$TMP_FILE" "${DEST}/ap-manager"
    ok "Instalado en: ${DEST}/ap-manager"

    # Verify — use --version (doesn't start the server)
    echo ""
    info "Verificando instalación..."
    # Bound execution when timeout is available; otherwise --version
    # terminates on its own since it does not start the server.
    VERIFY_CMD="timeout 10"
    command -v timeout >/dev/null 2>&1 || VERIFY_CMD=""
    VERIFY_OUTPUT=$(mktemp)
    if $VERIFY_CMD "${DEST}/ap-manager" --version >"$VERIFY_OUTPUT" 2>&1; then
        ok "AP Manager instalado correctamente"
    else
        warn "El binario se instaló pero no se pudo verificar su ejecución."
        if [ -s "$VERIFY_OUTPUT" ]; then
            warn "Salida de verificación: $(tr '\n' ' ' < "$VERIFY_OUTPUT")"
        fi
        warn "Prueba ejecutar: ${DEST}/ap-manager --version"
    fi
    rm -f "$VERIFY_OUTPUT"

    # Add to PATH if needed
    case ":$PATH:" in
        *":${DEST}:"*) ;;
        *)
            warn "${DEST} no está en tu PATH."
            case "$SHELL" in
                *zsh)
                    if ! grep -q "export PATH=\"${DEST}:\$PATH\"" "${HOME}/.zshrc" 2>/dev/null; then
                        echo "export PATH=\"${DEST}:\$PATH\"" >> "${HOME}/.zshrc"
                        ok "Añadido a ~/.zshrc. Ejecuta: source ~/.zshrc"
                    fi
                    ;;
                *bash)
                    if ! grep -q "export PATH=\"${DEST}:\$PATH\"" "${HOME}/.bashrc" 2>/dev/null; then
                        echo "export PATH=\"${DEST}:\$PATH\"" >> "${HOME}/.bashrc"
                        ok "Añadido a ~/.bashrc. Ejecuta: source ~/.bashrc"
                    fi
                    ;;
            esac
            ;;
    esac

    # Setup auto-start (optional)
    echo ""
    info "¿Quieres configurar inicio automático?"
    if [ "${OS}" = "android" ]; then
        ask "Crear script de inicio en Termux? (${HOME}/.termux/boot/) [s/N]: "
        resp=""
        read_input resp || true
        case "$resp" in
            s|S|y|Y)
                BOOT_DIR="${HOME}/.termux/boot"
                mkdir -p "$BOOT_DIR"
                cat > "${BOOT_DIR}/ap-manager" << 'EOF'
#!/data/data/com.termux/files/usr/bin/sh
termux-wake-lock
ap-manager &
EOF
                chmod 755 "${BOOT_DIR}/ap-manager"
                ok "Script de inicio creado en ${BOOT_DIR}/ap-manager"
                info "  Asegúrate de tener instalado termux-boot."
                info "  Ejecuta: pkg install termux-boot"
                ;;
        esac
    elif command -v systemctl >/dev/null 2>&1; then
        ask "Crear servicio systemd? [s/N]: "
        resp=""
        read_input resp || true
        case "$resp" in
            s|S|y|Y)
                SERVICE_DIR="${HOME}/.config/systemd/user"
                mkdir -p "$SERVICE_DIR"
                cat > "${SERVICE_DIR}/ap-manager.service" << EOF
[Unit]
Description=AP Manager - Multi-repository dashboard
After=network.target

[Service]
Type=simple
ExecStart=${DEST}/ap-manager
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
                ok "Servicio systemd creado en ${SERVICE_DIR}/ap-manager.service"
                info "  Para habilitar: systemctl --user enable ap-manager"
                info "  Para iniciar:   systemctl --user start ap-manager"
                ;;
        esac
    else
        info "  No se detectó systemd. Puedes ejecutar ap-manager manualmente."
        info "  Ejecuta: nohup ${DEST}/ap-manager > /dev/null 2>&1 &"
    fi

    # Summary
    echo ""
    info "╔══════════════════════════════════╗"
    info "║    Instalación completada        ║"
    info "╚══════════════════════════════════╝"
    echo ""
    info "  AP Manager ${TAG} (${SUFFIX})"
    info "  Binario: ${DEST}/ap-manager"
    info ""
    info "  Para ejecutar:"
    info "    ap-manager"
    info ""
    info "  Para acceder al dashboard:"
    info "    http://localhost:8080"
    echo ""
}

main "$@"
