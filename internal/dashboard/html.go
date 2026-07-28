// Package dashboard contains the embedded SPA HTML.
package dashboard

// HTML is the embedded single-page application for the AP Manager dashboard.
const HTML = `<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AP Manager</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
/* ═══════════════════════════════════════
   DESIGN TOKENS (Claude / Avena Warm Palette)
   ═══════════════════════════════════════ */
:root {
  --bg: #181614;
  --surface: #22201D;
  --surface-alt: #2A2724;
  --surface-hover: #33302C;
  --surface-elevated: #2E2B27;

  --border: rgba(236, 231, 223, 0.08);
  --border-light: rgba(236, 231, 223, 0.16);

  --text: #ECE7DF;
  --text-secondary: #B0A99F;
  --text-muted: #78726A;

  --accent: #D97757;             /* Claude Terracotta Accent */
  --accent-glow: rgba(217, 119, 87, 0.2);
  --accent-subtle: rgba(217, 119, 87, 0.12);
  --accent-hover: #E58868;

  --success: #7DAA81;
  --success-subtle: rgba(125, 170, 129, 0.12);
  --warning: #D9A05B;
  --warning-subtle: rgba(217, 160, 91, 0.12);
  --danger: #D96B6B;
  --danger-subtle: rgba(217, 107, 107, 0.12);

  --radius-sm: 4px;
  --radius: 8px;
  --radius-lg: 12px;

  --shadow-sm: 0 1px 3px rgba(0,0,0,0.3);
  --shadow: 0 4px 12px rgba(0,0,0,0.4);

  --font-sans: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-mono: 'JetBrains Mono', monospace;

  --transition: 150ms ease;
}

/* ═══════════════════════════════════════
   RESET & BASE
   ═══════════════════════════════════════ */
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
html { font-size: 14px; -webkit-font-smoothing: antialiased; }
body {
  font-family: var(--font-sans);
  background: var(--bg);
  color: var(--text);
  min-height: 100vh;
  line-height: 1.4;
}

/* SCROLLBAR */
::-webkit-scrollbar { width: 5px; height: 5px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: rgba(236,231,223,0.15); border-radius: 99px; }
::-webkit-scrollbar-thumb:hover { background: rgba(236,231,223,0.3); }

/* ═══════════════════════════════════════
   HEADER
   ═══════════════════════════════════════ */
.header {
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  padding: 0 16px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  position: sticky;
  top: 0;
  z-index: 50;
}
.header-left { display: flex; align-items: center; gap: 10px; }
.header-logo {
  width: 28px;
  height: 28px;
  background: var(--accent-subtle);
  border: 1px solid var(--accent);
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--accent);
  flex-shrink: 0;
}
.header-logo svg { width: 16px; height: 16px; }
.header-title { font-size: 0.95rem; font-weight: 700; color: var(--text); }
.header-title span { color: var(--accent); }
.header-sub { font-size: 0.7rem; color: var(--text-muted); font-weight: 500; display: none; }
@media(min-width:640px){ .header-sub { display: inline; margin-left: 6px; } }

.header-right { display: flex; align-items: center; gap: 8px; }
.header-meta {
  font-size: 0.7rem;
  color: var(--text-secondary);
  font-family: var(--font-mono);
  background: var(--surface-alt);
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
}

/* ═══════════════════════════════════════
   STATS BAR
   ═══════════════════════════════════════ */
.stats-bar {
  display: flex;
  border-bottom: 1px solid var(--border);
  background: var(--surface-alt);
  overflow-x: auto;
  padding: 0 12px;
}
.stat-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 16px;
  border-right: 1px solid var(--border);
  white-space: nowrap;
}
.stat-item:last-child { border-right: none; }
.stat-icon { width: 14px; height: 14px; color: var(--text-muted); }
.stat-value { font-size: 0.8rem; font-weight: 700; color: var(--text); font-family: var(--font-mono); }
.stat-label { font-size: 0.7rem; color: var(--text-muted); }

/* ═══════════════════════════════════════
   LAYOUT
   ═══════════════════════════════════════ */
.layout {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px;
  max-width: 1200px;
  margin: 0 auto;
}

/* ═══════════════════════════════════════
   REPO CARDS
   ═══════════════════════════════════════ */
.repos-section { display: flex; flex-direction: column; gap: 8px; }
.repos-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.repos-header h2 { font-size: 0.75rem; font-weight: 700; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.05em; }
.repos-count { font-size: 0.7rem; color: var(--accent); font-family: var(--font-mono); background: var(--accent-subtle); padding: 1px 6px; border-radius: 99px; margin-left: 6px; }
.repos-grid { display: flex; flex-direction: column; gap: 10px; }

.repo-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px;
  transition: border-color var(--transition);
}
.repo-card:hover { border-color: var(--border-light); }

.repo-card-top { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; gap: 8px; flex-wrap: wrap; }
.repo-card-title { font-size: 0.88rem; font-weight: 600; color: var(--text); display: flex; align-items: center; gap: 6px; }
.repo-card-title .repo-icon { width: 14px; height: 14px; color: var(--accent); flex-shrink: 0; }
.repo-card-title a { color: var(--text); text-decoration: none; }
.repo-card-title a:hover { color: var(--accent-hover); text-decoration: underline; }
.repo-card-title .owner { color: var(--text-muted); font-weight: 400; }
.repo-card-title .app-tag { font-weight: 500; color: var(--text-secondary); font-size: 0.7rem; background: var(--surface-alt); padding: 1px 6px; border-radius: 3px; border: 1px solid var(--border); }

.repo-card-badges { display: flex; gap: 4px; align-items: center; }

/* ═══════════════════════════════════════
   BADGES (Small Condensed)
   ═══════════════════════════════════════ */
.badge { display: inline-flex; align-items: center; gap: 4px; font-size: 0.65rem; font-weight: 600; padding: 2px 6px; border-radius: var(--radius-sm); white-space: nowrap; text-transform: uppercase; letter-spacing: 0.02em; }
.badge--running { background: var(--success-subtle); color: var(--success); border: 1px solid rgba(125,170,129,0.3); }
.badge--stopped { background: var(--surface-alt); color: var(--text-muted); border: 1px solid var(--border); }
.badge--busy { background: var(--accent-subtle); color: var(--accent); border: 1px solid rgba(217,119,87,0.3); }
.badge--completed { background: var(--success-subtle); color: var(--success); border: 1px solid rgba(125,170,129,0.3); }
.badge--failed { background: var(--danger-subtle); color: var(--danger); border: 1px solid rgba(217,107,107,0.3); }

/* ═══════════════════════════════════════
   VERSIONS DISPLAY
   ═══════════════════════════════════════ */
.versions { display: flex; gap: 12px; margin-bottom: 10px; background: var(--surface-alt); padding: 6px 10px; border-radius: var(--radius-sm); border: 1px solid var(--border); align-items: center; }
.version-box { display: flex; align-items: center; gap: 6px; flex: 1; }
.version-label { font-size: 0.65rem; text-transform: uppercase; color: var(--text-muted); font-weight: 600; }
.version-value { font-family: var(--font-mono); font-size: 0.8rem; font-weight: 600; }
.version-value--current { color: var(--text); }
.version-value--new { color: var(--accent-hover); }
.version-value--pending { color: var(--warning); }
.version-diff { display: inline-flex; align-items: center; gap: 2px; margin-left: 4px; font-size: 0.62rem; font-weight: 600; padding: 1px 4px; border-radius: 3px; }
.version-diff--same { color: var(--text-muted); background: var(--surface); }
.version-diff--new { color: var(--success); background: var(--success-subtle); }

/* ═══════════════════════════════════════
   ACTIONS / BUTTONS (Condensed & Small)
   ═══════════════════════════════════════ */
.actions { display: flex; gap: 4px; flex-wrap: wrap; align-items: center; }
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 3px 8px;
  font-size: 0.72rem;
  font-weight: 600;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  cursor: pointer;
  transition: all var(--transition);
  background: var(--surface-alt);
  color: var(--text-secondary);
  line-height: 1.2;
  white-space: nowrap;
  font-family: var(--font-sans);
  user-select: none;
}
.btn:hover { background: var(--surface-hover); border-color: var(--border-light); color: var(--text); }
.btn:active { transform: translateY(1px); }
.btn:disabled { opacity: 0.4; cursor: not-allowed; pointer-events: none; }
.btn--primary { background: var(--accent); color: #181614; border-color: var(--accent); font-weight: 700; }
.btn--primary:hover { background: var(--accent-hover); border-color: var(--accent-hover); color: #181614; }
.btn--success { background: var(--success); color: #181614; border-color: var(--success); font-weight: 700; }
.btn--success:hover { background: #8fbc93; color: #181614; }
.btn--danger { color: var(--danger); border-color: rgba(217, 107, 107, 0.3); background: var(--danger-subtle); }
.btn--danger:hover { background: rgba(217, 107, 107, 0.25); border-color: var(--danger); }
.btn--ghost { background: transparent; border-color: transparent; color: var(--text-muted); padding: 3px 5px; }
.btn--ghost:hover { background: var(--surface-hover); color: var(--text); }

.btn-icon svg { width: 12px; height: 12px; flex-shrink: 0; }

/* ═══════════════════════════════════════
   PROGRESS BAR
   ═══════════════════════════════════════ */
.progress-bar { width: 100%; height: 3px; background: var(--surface-alt); border-radius: 99px; overflow: hidden; margin-bottom: 8px; display: none; }
.progress-bar--active { display: block; }
.progress-bar-fill { height: 100%; background: var(--accent); border-radius: 99px; transition: width 300ms ease; width: 0%; }

/* ═══════════════════════════════════════
   CONSOLE LOG PANEL (Single Global Terminal)
   ═══════════════════════════════════════ */
.log-panel {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.log-panel-header { display: flex; align-items: center; justify-content: space-between; }
.log-panel-header h3 { font-size: 0.72rem; font-weight: 700; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.05em; display: flex; align-items: center; gap: 6px; }
.log-panel-header h3 svg { width: 12px; height: 12px; color: var(--accent); }
.log-console {
  background: #121110;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  height: 140px;
  overflow-y: auto;
  font-family: var(--font-mono);
  font-size: 0.7rem;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
.log-console .log-line { color: var(--text-secondary); margin-bottom: 1px; }
.log-console .log-line--err { color: var(--danger); }
.log-console .log-line--info { color: var(--accent-hover); }
.log-console .log-line--warn { color: var(--warning); }
.log-console .log-line--system { color: var(--text-muted); font-style: italic; }

/* ═══════════════════════════════════════
   ADD REPO MODAL
   ═══════════════════════════════════════ */
.modal-overlay { display: none; position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.6); z-index: 100; justify-content: center; align-items: center; backdrop-filter: blur(2px); }
.modal-overlay.active { display: flex; }
.modal { background: var(--surface); border: 1px solid var(--border-light); border-radius: var(--radius-lg); padding: 18px; max-width: 420px; width: 92%; box-shadow: var(--shadow); }
.modal-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.modal-header h2 { font-size: 0.95rem; font-weight: 700; color: var(--text); }
.modal-close { background: none; border: none; color: var(--text-muted); cursor: pointer; padding: 2px; }
.modal-close:hover { color: var(--text); }
.modal-close svg { width: 16px; height: 16px; }
.form-group { margin-bottom: 10px; }
.form-label { display: block; font-size: 0.72rem; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px; }
.form-input { width: 100%; padding: 6px 10px; background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-size: 0.8rem; outline: none; font-family: var(--font-sans); }
.form-input:focus { border-color: var(--accent); }
.form-select { appearance: none; background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6' fill='%2378726A'%3E%3Cpath d='M1 1l4 4 4-4'/%3E%3C/svg%3E"); background-repeat: no-repeat; background-position: right 8px center; padding-right: 26px; cursor: pointer; }
.form-select:disabled { opacity: 0.5; cursor: default; }
.form-hint { font-size: 0.68rem; color: var(--text-muted); margin-top: 4px; background: var(--surface-alt); padding: 4px 8px; border-radius: var(--radius-sm); border: 1px solid var(--border); }
.form-error { color: var(--danger); font-size: 0.72rem; margin-top: 6px; display: none; font-weight: 500; }
.modal-actions { display: flex; gap: 8px; margin-top: 16px; justify-content: flex-end; border-top: 1px solid var(--border); padding-top: 12px; }

/* ═══════════════════════════════════════
   TOAST
   ═══════════════════════════════════════ */
.toast-container { position: fixed; bottom: 16px; right: 16px; z-index: 200; display: flex; flex-direction: column; gap: 6px; max-width: 320px; }
.toast { background: var(--surface-elevated); border: 1px solid var(--border-light); border-radius: var(--radius-sm); padding: 8px 12px; font-size: 0.75rem; box-shadow: var(--shadow); display: flex; align-items: center; gap: 8px; }
.toast--success { border-color: rgba(125,170,129,0.4); }
.toast--error { border-color: rgba(217,107,107,0.4); }
.toast-icon { width: 14px; height: 14px; flex-shrink: 0; }
.toast--success .toast-icon { color: var(--success); }
.toast--error .toast-icon { color: var(--danger); }
.toast-message { flex: 1; color: var(--text); font-weight: 500; }

/* ═══════════════════════════════════════
   EMPTY STATE & SPINNER
   ═══════════════════════════════════════ */
.empty-state { text-align: center; padding: 32px 16px; background: var(--surface); border: 1px dashed var(--border); border-radius: var(--radius); }
.empty-state h3 { font-size: 0.95rem; font-weight: 700; color: var(--text); margin-bottom: 4px; }
.empty-state p { font-size: 0.75rem; color: var(--text-muted); margin-bottom: 12px; }

.spinner { display: inline-block; width: 10px; height: 10px; border: 2px solid currentColor; border-top-color: transparent; border-radius: 50%; animation: spin 0.6s linear infinite; vertical-align: middle; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>

<!-- ══════ HEADER ══════ -->
<header class="header">
  <div class="header-left">
    <div class="header-logo">
      <svg viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M9 2.5L3 6v6l6 3.5 6-3.5V6L9 2.5z"/>
        <path d="M3 6l6 3.5L15 6"/>
        <path d="M9 9.5v6"/>
      </svg>
    </div>
    <div>
      <span class="header-title"><span>AP</span> Manager</span>
      <span class="header-sub">Gestión de repositorios</span>
    </div>
  </div>
  <div class="header-right">
    <span class="header-meta"><span id="plat">--</span></span>
    <button class="btn btn--primary" onclick="showAddModal()">
      <svg width="12" height="12" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M7 2v10M2 7h10"/></svg>
      Añadir
    </button>
  </div>
</header>

<!-- ══════ STATS BAR ══════ -->
<div class="stats-bar" id="stats-bar">
  <div class="stat-item">
    <svg class="stat-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="1" y="1" width="12" height="12" rx="2"/><path d="M5 1v12"/><path d="M9 1v12"/><path d="M1 5h12"/><path d="M1 9h12"/></svg>
    <span class="stat-value" id="stat-repos">0</span>
    <span class="stat-label">repos</span>
  </div>
  <div class="stat-item">
    <svg class="stat-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 7l3.5 3.5L12 4"/></svg>
    <span class="stat-value" id="stat-running">0</span>
    <span class="stat-label">activos</span>
  </div>
  <div class="stat-item">
    <svg class="stat-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M7 1v6l2 2"/><circle cx="7" cy="7" r="6"/></svg>
    <span class="stat-value" id="stat-updates">0</span>
    <span class="stat-label">pendientes</span>
  </div>
  <div class="stat-item">
    <svg class="stat-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="7" cy="7" r="6"/><path d="M7 4v4l2.5 1.5"/></svg>
    <span class="stat-value" id="stat-uptime">--</span>
    <span class="stat-label">uptime</span>
  </div>
</div>

<!-- ══════ MAIN LAYOUT ══════ -->
<div class="layout">
  <!-- Repos List -->
  <div class="repos-section">
    <div class="repos-header" id="repos-header" style="display:none">
      <h2>Repositorios <span class="repos-count" id="repos-count">0</span></h2>
    </div>
    <div id="repos-container">
      <div class="empty-state" id="empty-state">
        <h3>No hay repositorios</h3>
        <p>Añade un repositorio para comenzar el seguimiento de versiones.</p>
        <button class="btn btn--primary" onclick="showAddModal()">
          <svg width="12" height="12" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2"><path d="M7 2v10M2 7h10"/></svg>
          Añadir repositorio
        </button>
      </div>
      <div class="repos-grid" id="repos-grid"></div>
    </div>
  </div>

  <!-- Single Unified Console / Log Panel -->
  <div class="log-panel">
    <div class="log-panel-header">
      <h3>
        <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1 3h10M1 6h8M1 9h6"/></svg>
        Registro de eventos
      </h3>
      <button class="btn btn--ghost" onclick="clearLog()" title="Limpiar registro">
        <svg width="11" height="11" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M1.5 3h9M2 3l1 8h6l1-8M4 3V2a1 1 0 011-1h2a1 1 0 011 1v1"/><path d="M4.5 5v4M7.5 5v4"/></svg>
        Limpiar log
      </button>
    </div>
    <div class="log-console" id="global-log">
      <div class="log-line log-line--system">AP Manager iniciado — esperando eventos...</div>
    </div>
  </div>
</div>

<!-- ══════ ADD MODAL ══════ -->
<div class="modal-overlay" id="add-modal">
  <div class="modal">
    <div class="modal-header">
      <h2>Añadir Repositorio</h2>
      <button class="modal-close" onclick="hideAddModal()">
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4l8 8M12 4l-8 8"/></svg>
      </button>
    </div>
    <div class="modal-form">
      <div class="form-group">
        <label class="form-label" for="in-owner">Usuario / Organización</label>
        <input class="form-input" id="in-owner" type="text" placeholder="ej. mfloresz" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="in-repo">Repositorio</label>
        <input class="form-input" id="in-repo" type="text" placeholder="ej. yara" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="in-app">Nombre del binario</label>
        <input class="form-input" id="in-app" type="text" placeholder="ej. translator-server" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label">Plataforma</label>
        <div style="display:flex;gap:8px;">
          <div style="flex:1">
            <input class="form-input" id="in-plat-os" type="text" readonly style="color:var(--text-muted);" placeholder="OS">
          </div>
          <div style="flex:1">
            <select class="form-input form-select" id="in-plat-arch"></select>
          </div>
        </div>
        <div class="form-hint" id="arch-hint">Arquitectura detectada automáticamente. Si aparece "arm", elige la variante correcta (armv7/arm64).</div>
      </div>
      <div class="form-group">
        <div class="form-hint" id="asset-preview">Asset esperado: <strong id="asset-name">translator-server-android-armv7</strong></div>
      </div>
      <div class="form-group">
        <label class="form-label" for="in-asset">Asset name exacto <span style="color:var(--text-muted);font-weight:400">(opcional)</span></label>
        <input class="form-input" id="in-asset" type="text" placeholder="ej. yara-v2.0.0-linux-amd64" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="in-args">Argumentos extra <span style="color:var(--text-muted);font-weight:400">(opcional)</span></label>
        <input class="form-input" id="in-args" type="text" placeholder="ej. --port 8080 --verbose" autocomplete="off">
        <div class="form-hint">Se pasarán al binario al iniciarlo (separados por espacios). Soporta $VAR</div>
      </div>
      <div class="form-group">
        <label class="form-label" for="in-install-path">Ruta de instalación <span style="color:var(--text-muted);font-weight:400">(opcional)</span></label>
        <input class="form-input" id="in-install-path" type="text" placeholder="auto: ~/bin/ o $PREFIX/bin" autocomplete="off">
        <div class="form-hint">Si se omite, se detecta automáticamente según la plataforma</div>
      </div>
      <div class="form-error" id="modal-error"></div>
    </div>
    <div class="modal-actions">
      <button class="btn" onclick="hideAddModal()">Cancelar</button>
      <button class="btn btn--primary" onclick="addRepo()">Añadir</button>
    </div>
  </div>
</div>

<!-- ══════ EDIT MODAL ══════ -->
<div class="modal-overlay" id="edit-modal">
  <div class="modal">
    <div class="modal-header">
      <h2>Editar Repositorio</h2>
      <button class="modal-close" onclick="hideEditModal()">
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4l8 8M12 4l-8 8"/></svg>
      </button>
    </div>
    <div class="modal-form">
      <div class="form-group">
        <label class="form-label" for="edit-id">ID del repositorio</label>
        <input class="form-input" id="edit-id" type="text" readonly style="color:var(--text-muted);">
      </div>
      <div class="form-group">
        <label class="form-label" for="edit-app">Nombre del binario</label>
        <input class="form-input" id="edit-app" type="text" placeholder="ej. translator-server" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="edit-asset">Asset name exacto <span style="color:var(--text-muted);font-weight:400">(vacío = auto)</span></label>
        <input class="form-input" id="edit-asset" type="text" placeholder="ej. translator-server-android-armv7-v0.7.1" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label" for="edit-args">Argumentos extra <span style="color:var(--text-muted);font-weight:400">(vacío = ninguno)</span></label>
        <input class="form-input" id="edit-args" type="text" placeholder="ej. --port 8080 --verbose" autocomplete="off">
        <div class="form-hint">Se pasarán al binario al iniciarlo. Soporta $VAR como $HOME</div>
      </div>
      <div class="form-group">
        <label class="form-label" for="edit-install-path">Ruta de instalación <span style="color:var(--text-muted);font-weight:400">(vacío = auto)</span></label>
        <input class="form-input" id="edit-install-path" type="text" placeholder="auto: ~/bin/ o $PREFIX/bin" autocomplete="off">
      </div>
      <div class="form-group">
        <label class="form-label">Plataforma</label>
        <div style="display:flex;gap:8px;">
          <div style="flex:1">
            <input class="form-input" id="edit-plat-os" type="text" readonly style="color:var(--text-muted);" placeholder="OS">
          </div>
          <div style="flex:1">
            <select class="form-input form-select" id="edit-plat-arch"></select>
          </div>
        </div>
        <div class="form-hint">Cambia la arquitectura si el asset detectado no es correcto.</div>
      </div>
      <div class="form-error" id="edit-modal-error"></div>
    </div>
    <div class="modal-actions">
      <button class="btn" onclick="hideEditModal()">Cancelar</button>
      <button class="btn btn--primary" onclick="saveEditRepo()">Guardar cambios</button>
    </div>
  </div>
</div>

<!-- ══════ TOAST CONTAINER ══════ -->
<div class="toast-container" id="toast-container"></div>

<script>
// STATE
var repos = [];
var repoStatus = {};
var repoSvc = {};
var repoLatestVer = {};
var repoCurrentVer = {};
var repoProgress = {};
var repoInstalled = {};
var detectedOS = '';
var detectedArch = '';

// INIT
fetch('/api/platform').then(function(r){return r.json()}).then(function(d){
  detectedOS = d.os;
  detectedArch = d.arch;
  document.getElementById('plat').textContent = d.os + '/' + d.arch;
  document.getElementById('in-plat-os').value = d.os;
  populateArchSelect('in-plat-arch', d.arch);
  updateAssetPreview();
}).catch(function(){});

loadRepos();

document.getElementById('in-app').addEventListener('input', updateAssetPreview);
document.getElementById('in-plat-arch').addEventListener('change', updateAssetPreview);

function populateArchSelect(selectId, currentArch) {
  var sel = document.getElementById(selectId);
  if (!sel) return;
  sel.innerHTML = '';
  var options = [];
  if (currentArch === 'arm') {
    options = ['arm', 'armv7', 'arm64'];
    sel.disabled = false;
  } else {
    options = [currentArch];
    sel.disabled = true;
  }
  options.forEach(function(a) {
    var opt = document.createElement('option');
    opt.value = a;
    opt.textContent = a;
    if (a === currentArch) opt.selected = true;
    sel.appendChild(opt);
  });
}

function updateAssetPreview() {
  var app = document.getElementById('in-app').value || 'translator-server';
  var os = document.getElementById('in-plat-os').value || detectedOS;
  var arch = document.getElementById('in-plat-arch').value || detectedArch;
  document.getElementById('asset-name').textContent = app + '-' + os + '-' + arch;
}

// ICONS
var ICONS = {
  repo: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="2" y="2" width="10" height="10" rx="1.5"/><path d="M5 5h4"/><path d="M5 7.5h3"/><path d="M5 10h2"/></svg>',
  check: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="7" cy="7" r="5.5"/><path d="M4.5 7l1.5 1.5L9.5 5.5"/></svg>',
  download: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M7 1.5v8M3.5 6.5L7 9.5l3.5-3"/><path d="M1.5 9v2.5a1 1 0 001 1h9a1 1 0 001-1V9"/></svg>',
  restart: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2.5 8.5a5.5 5.5 0 0010-1.5M2 5a5.5 5.5 0 019 2"/><path d="M2.5 2.5V5H5"/><path d="M11.5 11.5V9H9"/></svg>',
  stop: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="7" cy="7" r="5.5"/><rect x="5" y="5" width="4" height="4" rx=".5" fill="currentColor"/></svg>',
  start: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="7" cy="7" r="5.5"/><path d="M5.5 4.5v5l4-2.5-4-2.5z" fill="currentColor"/></svg>',
  remove: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="7" cy="7" r="5.5"/><path d="M4.5 4.5l5 5M9.5 4.5l-5 5"/></svg>',
  plus: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2"><path d="M7 2v10M2 7h10"/></svg>',
  edit: '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M10.5 1.5l2 2L4 12H2v-2l8.5-8.5z"/></svg>',
  running: '<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6" cy="6" r="5"/><path d="M4 6l1.5 1.5L8 4"/></svg>',
  stopped: '<svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="6" cy="6" r="5"/><path d="M4 4l4 4M8 4l-4 4"/></svg>'
};

// GLOBAL SSE & LOG
var globalLog = document.getElementById('global-log');
var globalSrc = new EventSource('/api/events/global');

function appendGlobalLog(msg, type, timestamp) {
  var line = document.createElement('div');
  line.className = 'log-line';
  if (type === 'error' || (msg && msg.indexOf('ERROR') !== -1)) line.className += ' log-line--err';
  else if (type === 'system') line.className += ' log-line--system';
  else if (type === 'warn') line.className += ' log-line--warn';
  else line.className += ' log-line--info';

  var ts = timestamp ? '[' + timestamp + '] ' : '';
  line.textContent = ts + msg;
  globalLog.appendChild(line);
  globalLog.scrollTop = globalLog.scrollHeight;
}

globalSrc.onmessage = function(e) {
  try {
    var evt = JSON.parse(e.data);
    if (evt.message) {
      appendGlobalLog(evt.message, evt.type, evt.timestamp);
    }
  } catch(err) {
    if (e.data) appendGlobalLog(e.data, 'info');
  }
};

// LOAD REPOS
function loadRepos() {
  fetch('/api/repos')
    .then(function(r){return r.json()})
    .then(function(data) {
      repos = data;
  	    repos.forEach(function(repo) {
	        if (!repoStatus[repo.id]) repoStatus[repo.id] = 'idle';
	        if (repo.current_version) repoCurrentVer[repo.id] = repo.current_version;
	        if (repo.latest_version) repoLatestVer[repo.id] = repo.latest_version;
	        if (repo.progress) repoProgress[repo.id] = repo.progress;
	        repoInstalled[repo.id] = repo.installed ? true : false;
	        if (['completed', 'failed'].indexOf(repoStatus[repo.id]) !== -1) {
	          repoStatus[repo.id] = 'idle';
	        }
	        connectRepoSSE(repo.id);
	      });
      renderRepos();
      fetchServiceStatuses();
      startServicePolling();
    })
    .catch(function() {
      showToast('Error al cargar repositorios', 'error');
    });
}

// PER-REPO SSE (Routes log output to the main log console)
var repoSSEs = {};

function safeId(id) {
  return id.replace(/[\/\s]/g, '_');
}

function connectRepoSSE(repoId) {
  if (repoSSEs[repoId]) return;
  var src = new EventSource('/api/events?id=' + encodeURIComponent(repoId));
  repoSSEs[repoId] = src;

  src.onmessage = function(e) {
    var sid = safeId(repoId);

    try {
      var evt = JSON.parse(e.data);

      if (evt.type === 'progress') {
        if (evt.percent !== undefined) {
          repoProgress[repoId] = evt.percent;
          var barEl = document.getElementById('progress-' + sid);
          if (barEl) {
            barEl.style.width = evt.percent + '%';
            var wrapper = barEl.closest('.progress-bar');
            if (wrapper) wrapper.classList.add('progress-bar--active');
          }
        }
        if (evt.step === 'download' && evt.percent !== undefined) {
          updateStatusBadge(repoId, 'downloading');
        }
      }
    	  else if (evt.type === 'version') {
    	    if (evt.current) {
    	      repoCurrentVer[repoId] = evt.current;
    	      var curEl = document.getElementById('current-' + sid);
    	      if (curEl) curEl.textContent = evt.current;
    	    }
    	    if (evt.latest) {
    	      repoLatestVer[repoId] = evt.latest;
    	      var newEl = document.getElementById('newver-' + sid);
    	      if (newEl) {
    	        newEl.textContent = evt.latest;
    	        var cls = 'version-value version-value--new';
    	        if (repoLatestVer[repoId] !== repoCurrentVer[repoId]) cls += ' version-value--pending';
    	        newEl.className = cls;
    	      }
    	      updateVersionDiff(repoId, sid);
    	    }
    	    // Refresh to sync installed state (CheckVersion may have found binary)
    	    setTimeout(function() { loadRepos(); }, 300);
    	  }
    	  if (evt.type === 'status') {
    	    if (evt.status) {
    	      var mappedStatus = mapBackendStatus(evt.status);
    	      repoStatus[repoId] = mappedStatus;
    	      updateStatusBadge(repoId, mappedStatus);
    	      updateButtons(repoId, mappedStatus);

    	      if (mappedStatus === 'completed') {
    	        repoInstalled[repoId] = true;
    	        showToast('Instalación completada para ' + repoId, 'success');
    	        setTimeout(function() { loadRepos(); }, 1000);
    	      }
    	      if (mappedStatus === 'failed') {
    	        showToast('Error en operación para ' + repoId, 'error');
    	      }
    	    }

        if (evt.service_run !== undefined) {
          repoSvc[repoId] = evt.service_run;
          updateSvcBadge(repoId, sid);
          updateButtons(repoId, repoStatus[repoId]);
          updateStats();
        }
      }

      // Output repo logs directly into main log console
      if (evt.message) {
        appendGlobalLog('[' + repoId + '] ' + evt.message, evt.type, evt.timestamp);
      }

    } catch(err) {
      if (e.data) appendGlobalLog('[' + repoId + '] ' + e.data, 'info');
    }
  };
}

function mapBackendStatus(st) {
  switch(st) {
    case 'checking': return 'checking';
    case 'downloading': return 'downloading';
    case 'installing': return 'installing';
    case 'completed': return 'completed';
    case 'failed': return 'failed';
    default: return 'idle';
  }
}

// RENDER REPOS
function renderRepos() {
  var container = document.getElementById('repos-grid');
  var emptyState = document.getElementById('empty-state');
  var header = document.getElementById('repos-header');
  var countEl = document.getElementById('repos-count');

  if (!repos || repos.length === 0) {
    container.innerHTML = '';
    emptyState.style.display = 'block';
    header.style.display = 'none';
    updateStats();
    return;
  }

  emptyState.style.display = 'none';
  header.style.display = 'flex';
  countEl.textContent = repos.length;

  var html = '';
	  repos.forEach(function(repo) {
	    var sid = safeId(repo.id);
	    var cur = repoCurrentVer[repo.id] || repo.current_version || '--';
	    var lat = repoLatestVer[repo.id] || repo.latest_version || '--';
	    var st = repoStatus[repo.id] || 'idle';
	    var svc = repoSvc[repo.id];
	    var installed = repoInstalled[repo.id];
	    var busyStates = ['checking','downloading','stopping','replacing','starting','verifying'];

	    html += '<div class="repo-card" id="card-' + sid + '">';

	    // Top Header
	    html += '  <div class="repo-card-top">';
	    html += '    <div class="repo-card-title">';
	    html += '      <span class="repo-icon">' + ICONS.repo + '</span>';
	    html += '      <a href="https://github.com/' + repo.owner + '/' + repo.name + '" target="_blank" rel="noopener"><span class="owner">' + repo.owner + '/</span>' + repo.name + '</a>';
	    html += '      <span class="app-tag">' + (repo.app_name || repo.name) + '</span>';
	    html += '    </div>';
	    html += '    <div class="repo-card-badges">';
	    html += getSvcBadgeHtml(svc);
	    if (!installed) {
	      if (busyStates.indexOf(st) !== -1) {
	        html += '<span class="badge badge--busy"><span class="spinner"></span> instalando</span>';
	      } else {
	        html += '<span class="badge badge--stopped">no instalado</span>';
	      }
	    } else {
	      html += getStatusBadgeHtml(st);
	    }
	    html += '    </div>';
	    html += '  </div>';

	    // Versions
	    html += '  <div class="versions">';
	    html += '    <div class="version-box">';
	    html += '      <span class="version-label">Actual:</span>';
	    html += '      <span class="version-value version-value--current" id="current-' + sid + '">' + cur + '</span>';
	    html += '    </div>';
	    html += '    <div class="version-box">';
	    html += '      <span class="version-label">Última:</span>';
	    var latCls = 'version-value version-value--new';
	    if (lat !== cur && lat !== '--') latCls += ' version-value--pending';
	    html += '      <span class="' + latCls + '" id="newver-' + sid + '">' + lat + '</span><span id="diff-' + sid + '"></span>';
	    html += '    </div>';
	    html += '  </div>';

	    // Progress Bar
	    var prog = repoProgress[repo.id] || 0;
	    var progActive = (busyStates.indexOf(st) !== -1 || prog > 0) ? ' progress-bar--active' : '';
	    html += '  <div class="progress-bar' + progActive + '"><div class="progress-bar-fill" id="progress-' + sid + '" style="width:' + prog + '%"></div></div>';

	    // Actions
	    html += '  <div class="actions" id="actions-' + sid + '">';
	    html += getButtonsHtml(repo.id, st, svc);
	    html += '  </div>';

	    html += '</div>';
	  });

  container.innerHTML = html;

  repos.forEach(function(repo) {
    updateVersionDiff(repo.id, safeId(repo.id));
  });

  updateStats();
}

function getSvcBadgeHtml(svc) {
  if (svc === 'running') return '<span class="badge badge--running">' + ICONS.running + ' activo</span>';
  if (svc === 'stopped') return '<span class="badge badge--stopped">' + ICONS.stopped + ' detenido</span>';
  return '';
}

function getStatusBadgeHtml(st) {
  switch(st) {
    case 'checking': return '<span class="badge badge--busy"><span class="spinner"></span> buscando</span>';
    case 'downloading': return '<span class="badge badge--busy"><span class="spinner"></span> descargando</span>';
    case 'installing': return '<span class="badge badge--busy"><span class="spinner"></span> instalando</span>';
    case 'completed': return '<span class="badge badge--completed">' + ICONS.check + ' listo</span>';
    case 'failed': return '<span class="badge badge--failed">error</span>';
    default: return '';
  }
}

function getButtonsHtml(repoId, st, svc) {
  var busyStates = ['checking','downloading','stopping','replacing','starting','verifying'];
  var isBusy = busyStates.indexOf(st) !== -1;
  var isInstalled = repoInstalled[repoId];
  var disabled = isBusy ? 'disabled' : '';

  // ── No instalado: solo buscar, instalar, eliminar ──
  if (!isInstalled) {
    var h = '';
    h += '<button class="btn" onclick="checkRepo(\'' + repoId + '\')" ' + disabled + ' title="Buscar versi\u00f3n disponible">' + ICONS.check + ' Buscar</button>';
    h += '<button class="btn btn--primary" onclick="installRepo(\'' + repoId + '\')" ' + disabled + ' title="Descargar e instalar binario">' + ICONS.download + ' Instalar</button>';
    h += '<button class="btn" onclick="editRepo(\'' + repoId + '\')" title="Editar configuraci\u00f3n">' + ICONS.edit + ' Editar</button>';
    h += '<button class="btn" onclick="removeRepo(\'' + repoId + '\')" ' + disabled + ' title="Eliminar repositorio">' + ICONS.remove + ' Eliminar</button>';
    return h;
  }

  // ── Instalado: conjunto completo de acciones ──
  var h = '';
  h += '<button class="btn" onclick="checkRepo(\'' + repoId + '\')" ' + disabled + ' title="Buscar actualizaci\u00f3n">' + ICONS.check + ' Buscar</button>';

  var cur = repoCurrentVer[repoId];
  var lat = repoLatestVer[repoId];
  var hasUpdate = (lat && cur && lat !== cur && lat !== '--' && cur !== '--');

  h += '<button class="btn ' + (hasUpdate ? 'btn--success' : '') + '" onclick="updateRepo(\'' + repoId + '\')" ' + disabled + ' title="Actualizar binario">' + ICONS.download + ' Actualizar</button>';

  if (svc === 'running') {
    h += '<button class="btn btn--danger" onclick="stopService(\'' + repoId + '\')" ' + disabled + ' title="Detener servicio">' + ICONS.stop + ' Detener</button>';
    h += '<button class="btn" onclick="restartService(\'' + repoId + '\')" ' + disabled + ' title="Reiniciar servicio">' + ICONS.restart + ' Reiniciar</button>';
  } else if (svc === 'stopped') {
    h += '<button class="btn" onclick="startService(\'' + repoId + '\')" ' + disabled + ' title="Iniciar servicio">' + ICONS.start + ' Iniciar</button>';
  } else {
    h += '<button class="btn" onclick="startService(\'' + repoId + '\')" ' + disabled + ' title="Iniciar servicio">' + ICONS.start + ' Iniciar</button>';
    h += '<button class="btn" onclick="restartService(\'' + repoId + '\')" ' + disabled + ' title="Reiniciar servicio">' + ICONS.restart + ' Reiniciar</button>';
  }

  h += '<button class="btn" onclick="removeRepo(\'' + repoId + '\')" ' + disabled + ' title="Eliminar repositorio">' + ICONS.remove + ' Eliminar</button>';

  h += '<button class="btn" onclick="editRepo(\'' + repoId + '\')" title="Editar configuraci\u00f3n">' + ICONS.edit + ' Editar</button>';

  return h;
}

function updateStatusBadge(repoId, st) {
  var sid = safeId(repoId);
  var card = document.getElementById('card-' + sid);
  if (!card) return;
  var badges = card.querySelector('.repo-card-badges');
  if (!badges) return;
  var busyStates = ['checking','downloading','stopping','replacing','starting','verifying'];
  var svcHtml = getSvcBadgeHtml(repoSvc[repoId]);
  var statusHtml = '';
  if (!repoInstalled[repoId]) {
    if (busyStates.indexOf(st) !== -1) {
      statusHtml = '<span class="badge badge--busy"><span class="spinner"></span> instalando</span>';
    } else {
      statusHtml = '<span class="badge badge--stopped">no instalado</span>';
    }
  } else {
    statusHtml = getStatusBadgeHtml(st);
  }
  badges.innerHTML = svcHtml + statusHtml;
}

function updateSvcBadge(repoId, sid) {
  updateStatusBadge(repoId, repoStatus[repoId] || 'idle');
}

function updateButtons(repoId, st) {
  var sid = safeId(repoId);
  var act = document.getElementById('actions-' + sid);
  if (act) act.innerHTML = getButtonsHtml(repoId, st, repoSvc[repoId]);
}

function updateVersionDiff(repoId, sid) {
  var diffEl = document.getElementById('diff-' + sid);
  if (!diffEl) return;
  var cur = repoCurrentVer[repoId];
  var lat = repoLatestVer[repoId];

  if (!cur || !lat || cur === '--' || lat === '--') {
    diffEl.innerHTML = '';
    return;
  }

  if (cur === lat) {
    diffEl.innerHTML = '<span class="version-diff version-diff--same">al día</span>';
  } else {
    diffEl.innerHTML = '<span class="version-diff version-diff--new">' + ICONS.plus + 'nueva</span>';
  }
}

function updateStats() {
  document.getElementById('stat-repos').textContent = repos.length;

  var activeCount = 0;
  var updatesCount = 0;

  repos.forEach(function(r) {
    if (repoSvc[r.id] === 'running') activeCount++;

    var cur = repoCurrentVer[r.id] || r.current_version;
    var lat = repoLatestVer[r.id] || r.latest_version;
    if (cur && lat && cur !== lat && cur !== '--' && lat !== '--') {
      updatesCount++;
    }
  });

  document.getElementById('stat-running').textContent = activeCount;
  document.getElementById('stat-updates').textContent = updatesCount;
}

// UPTIME COUNTER
var startTime = Date.now();
setInterval(function() {
  var elapsed = Math.floor((Date.now() - startTime) / 1000);
  var h = Math.floor(elapsed / 3600);
  var m = Math.floor((elapsed % 3600) / 60);
  var s = elapsed % 60;
  var str = '';
  if (h > 0) str += h + 'h ';
  str += m + 'm ' + s + 's';
  document.getElementById('stat-uptime').textContent = str;
}, 1000);

// SERVICE POLLING
function fetchServiceStatuses() {
  repos.forEach(function(r) {
    fetch('/api/repos/status?id=' + encodeURIComponent(r.id))
      .then(function(res){return res.json()})
      .then(function(d){
        if (d.status) {
          repoSvc[r.id] = d.status;
        }
        updateSvcBadge(r.id, safeId(r.id));
        updateButtons(r.id, repoStatus[r.id] || 'idle');
        updateStats();
      }).catch(function(){});
  });
}

function startServicePolling() {
  setInterval(function() {
    repos.forEach(function(r) {
      fetch('/api/repos/status?id=' + encodeURIComponent(r.id))
        .then(function(res){return res.json()})
        .then(function(d){
          if (d.status) {
            repoSvc[r.id] = d.status;
            updateSvcBadge(r.id, safeId(r.id));
            updateButtons(r.id, repoStatus[r.id] || 'idle');
            updateStats();
          }
        }).catch(function(){});
    });
  }, 10000);
}

// ACTIONS API
function checkRepo(id) {
  repoStatus[id] = 'checking';
  updateStatusBadge(id, 'checking');
  updateButtons(id, 'checking');
  connectRepoSSE(id);
  fetch('/api/repos/check?id=' + encodeURIComponent(id))
    .then(function(r){return r.json()})
    .then(function(data) {
      if (data.status === 'checking') {
        showToast('Búsqueda iniciada para ' + id, 'success');
      }
    })
    .catch(function(){
      repoStatus[id] = 'idle';
      updateStatusBadge(id, 'idle');
      updateButtons(id, 'idle');
      showToast('Error al buscar actualización', 'error');
    });
}

function updateRepo(id) {
  repoStatus[id] = 'checking';
  updateStatusBadge(id, 'checking');
  updateButtons(id, 'checking');
  connectRepoSSE(id);
  fetch('/api/repos/update?id=' + encodeURIComponent(id))
    .then(function(r){return r.json()})
    .then(function(data) {
      if (data.status === 'updating') {
        showToast('Actualización iniciada para ' + id, 'success');
        setTimeout(function() {
          var stuck = ['checking','downloading','stopping','replacing','starting','verifying'];
          if (stuck.indexOf(repoStatus[id]) !== -1) {
            repoStatus[id] = 'failed';
            updateStatusBadge(id, 'failed');
            showToast('La actualización tardó demasiado', 'error');
            setTimeout(function() {
              repoStatus[id] = 'idle';
              updateStatusBadge(id, 'idle');
              updateButtons(id, 'idle');
            }, 4000);
          }
        }, 180000);
      }
    })
    .catch(function(){
      repoStatus[id] = 'idle';
      updateStatusBadge(id, 'idle');
      updateButtons(id, 'idle');
      showToast('Error al iniciar actualización', 'error');
    });
}

function startService(id) {
  fetch('/api/repos/start',{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({id:id})
  })
    .then(function(r){return r.json()})
    .then(function(d){
      if (d.status === 'started' || d.status === 'already_running') {
        repoSvc[id] = 'running';
        updateSvcBadge(id, safeId(id));
        updateButtons(id, repoStatus[id] || 'idle');
        updateStats();
        showToast('Servicio iniciado: ' + id, 'success');
      } else {
        showToast('Error al iniciar: ' + (d.message || d.status), 'error');
      }
    })
    .catch(function(err){
      showToast('Error al iniciar: ' + err.message, 'error');
    });
}

	function installRepo(id) {
	  repoStatus[id] = 'checking';
	  updateStatusBadge(id, 'checking');
	  updateButtons(id, 'checking');
	  connectRepoSSE(id);
	  fetch('/api/repos/install', {
	    method:'POST',
	    headers:{'Content-Type':'application/json'},
	    body:JSON.stringify({id:id})
	  })
	    .then(function(r){return r.json()})
	    .then(function(data) {
	      if (data.status === 'installing') {
	        showToast('Instalación iniciada para ' + id, 'success');
	      } else if (data.status === 'already_installed') {
	        repoInstalled[id] = true;
	        updateStatusBadge(id, 'idle');
	        updateButtons(id, 'idle');
	        showToast(id + ' ya está instalado', 'success');
	      }
	    })
	    .catch(function(){
	      repoStatus[id] = 'idle';
	      updateStatusBadge(id, 'idle');
	      updateButtons(id, 'idle');
	      showToast('Error al iniciar instalación', 'error');
	    });
	}

	function stopService(id) {
  fetch('/api/repos/stop',{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({id:id})
  })
    .then(function(r){return r.json()})
    .then(function(d){
      if (d.status === 'stopped') {
        repoSvc[id] = 'stopped';
        updateSvcBadge(id, safeId(id));
        updateButtons(id, repoStatus[id] || 'idle');
        updateStats();
        showToast('Servicio detenido: ' + id, 'success');
      } else {
        showToast('Error al detener: ' + (d.message || d.status), 'error');
      }
    })
    .catch(function(err){
      showToast('Error al detener: ' + err.message, 'error');
    });
}

function restartService(id) {
  fetch('/api/repos/restart',{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({id:id})
  })
    .then(function(r){return r.json()})
    .then(function(d){
      if (d.status === 'restarted') {
        repoSvc[id] = 'running';
        updateSvcBadge(id, safeId(id));
        updateButtons(id, repoStatus[id] || 'idle');
        updateStats();
        showToast('Servicio reiniciado: ' + id, 'success');
      } else {
        showToast('Error al reiniciar: ' + (d.message || d.status), 'error');
      }
    })
    .catch(function(err){
      showToast('Error al reiniciar: ' + err.message, 'error');
    });
}

function removeRepo(id) {
  if (!confirm('¿Estás seguro de eliminar el repositorio ' + id + '?')) return;
  fetch('/api/repos/remove', {
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({id:id})
  })
    .then(function(r){
      if (!r.ok) throw new Error('HTTP ' + r.status);
    	  if (repoSSEs[id]) {
    	    repoSSEs[id].close();
    	    delete repoSSEs[id];
    	  }
    	  delete repoStatus[id];
    	  delete repoSvc[id];
    	  delete repoLatestVer[id];
    	  delete repoCurrentVer[id];
    	  delete repoProgress[id];
    	  delete repoInstalled[id];
      showToast('Repositorio eliminado', 'success');
      loadRepos();
    })
	    .catch(function(err){ showToast('Error al eliminar: ' + err.message, 'error'); });
}

// EDIT REPO
var editingRepoId = null;

function editRepo(id) {
  editingRepoId = id;
  var repo = repos.find(function(r){ return r.id === id; });
  if (!repo) { showToast('Repositorio no encontrado', 'error'); return; }

  document.getElementById('edit-id').value = id;
  document.getElementById('edit-app').value = repo.app_name || '';
  document.getElementById('edit-asset').value = repo.asset_name || '';
  document.getElementById('edit-args').value = repo.custom_command || '';
  document.getElementById('edit-install-path').value = repo.install_path || '';

  // Platform
  var os = repo.platform_os || detectedOS;
  document.getElementById('edit-plat-os').value = os;
  populateArchSelect('edit-plat-arch', repo.platform_arch || detectedArch);

  document.getElementById('edit-modal-error').style.display = 'none';
  document.getElementById('edit-modal').classList.add('active');
}

function hideEditModal() {
  document.getElementById('edit-modal').classList.remove('active');
  editingRepoId = null;
}

function saveEditRepo() {
  var id = editingRepoId;
  if (!id) return;

  var errEl = document.getElementById('edit-modal-error');
  var body = { id: id };

  var appName = document.getElementById('edit-app').value.trim();
  if (appName) body.app_name = appName;

  var asset = document.getElementById('edit-asset').value.trim();
  // Only send if non-empty, otherwise keep auto-detect
  if (asset) body.asset = asset;

  // Platform overrides (always send to allow resetting to detected)
  var arch = document.getElementById('edit-plat-arch').value;
  if (arch) body.platform_arch = arch;

  var args = document.getElementById('edit-args').value.trim();
  if (args) body.custom_command = args;

  var installPath = document.getElementById('edit-install-path').value.trim();
  if (installPath) body.install_path = installPath;

  fetch('/api/repos/edit', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  })
  .then(function(r){
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return r.json();
  })
  .then(function(data){
    hideEditModal();
    showToast('Repositorio actualizado: ' + id, 'success');
    loadRepos();
  })
  .catch(function(err){
    errEl.textContent = err.message;
    errEl.style.display = 'block';
  });
}

// MODAL & TOAST
function showAddModal() {
  document.getElementById('add-modal').classList.add('active');
  document.getElementById('in-owner').focus();
  // Reset platform fields to detected values
  document.getElementById('in-plat-os').value = detectedOS;
  populateArchSelect('in-plat-arch', detectedArch);
  updateAssetPreview();
}

function hideAddModal() {
  document.getElementById('add-modal').classList.remove('active');
  document.getElementById('in-owner').value = '';
  document.getElementById('in-repo').value = '';
  document.getElementById('in-app').value = '';
  document.getElementById('in-asset').value = '';
  document.getElementById('in-args').value = '';
  document.getElementById('in-install-path').value = '';
  document.getElementById('modal-error').style.display = 'none';
}

function addRepo() {
  var owner = document.getElementById('in-owner').value.trim();
  var name = document.getElementById('in-repo').value.trim();
  var appName = document.getElementById('in-app').value.trim();
  var asset = document.getElementById('in-asset').value.trim();
  var errEl = document.getElementById('modal-error');

  if (!owner || !name || !appName) {
    errEl.textContent = 'Completa todos los campos obligatorios';
    errEl.style.display = 'block';
    return;
  }

  var args = document.getElementById('in-args').value.trim();
  var installPath = document.getElementById('in-install-path').value.trim();

  var body = {
    owner: owner,
    name: name,
    app_name: appName,
    platform_os: document.getElementById('in-plat-os').value || detectedOS,
    platform_arch: document.getElementById('in-plat-arch').value || detectedArch
  };
  if (asset) body.asset = asset;
  if (args) body.custom_command = args;
  if (installPath) body.install_path = installPath;

  fetch('/api/repos/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  })
  .then(function(r){
    if (r.status === 409) throw new Error('El repositorio ya existe');
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return r.json();
  })
  .then(function(data){
    hideAddModal();
    document.getElementById('in-owner').value = '';
    document.getElementById('in-repo').value = '';
    document.getElementById('in-app').value = '';
    document.getElementById('in-asset').value = '';
    showToast('Repositorio añadido: ' + data.owner + '/' + data.name, 'success');
    loadRepos();
    connectRepoSSE(data.id);
  })
  .catch(function(err){
    errEl.textContent = err.message;
    errEl.style.display = 'block';
  });
}

function showToast(msg, type) {
  var container = document.getElementById('toast-container');
  var toast = document.createElement('div');
  toast.className = 'toast toast--' + (type || 'success');

  var icon = type === 'error' ? ICONS.remove : ICONS.check;
  toast.innerHTML = '<span class="toast-icon">' + icon + '</span><span class="toast-message">' + msg + '</span>';

  container.appendChild(toast);
  setTimeout(function() {
    toast.style.opacity = '0';
    toast.style.transition = 'opacity 0.2s';
    setTimeout(function() { toast.remove(); }, 200);
  }, 3000);
}

function clearLog() {
  globalLog.innerHTML = '<div class="log-line log-line--system">Registro limpiado. Esperando eventos...</div>';
}
</script>
</body>
</html>`
