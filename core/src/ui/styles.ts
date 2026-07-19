export const css = `
:root {
  --bg: #ffffff;
  --bg-secondary: #f9fafb;
  --bg-tertiary: #f3f4f6;
  --text: #111827;
  --text-secondary: #6b7280;
  --text-muted: #9ca3af;
  --border: #e5e7eb;
  --border-strong: #d1d5db;
  --accent: #2563eb;
  --accent-light: #eff6ff;
  --accent-text: #1d4ed8;
  --success: #16a34a;
  --success-light: #f0fdf4;
  --warning: #d97706;
  --warning-light: #fffbeb;
  --danger: #dc2626;
  --danger-light: #fef2f2;
  --radius-sm: 6px;
  --radius: 8px;
  --radius-lg: 12px;
  --shadow-sm: 0 1px 2px rgba(0,0,0,0.05);
  --shadow: 0 1px 3px rgba(0,0,0,0.1), 0 1px 2px rgba(0,0,0,0.06);
  --font: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
  --sidebar-w: 240px;
  --topbar-h: 56px;
}

@media (prefers-color-scheme: dark) {
  :root {
    --bg: #111827;
    --bg-secondary: #1f2937;
    --bg-tertiary: #374151;
    --text: #f9fafb;
    --text-secondary: #9ca3af;
    --text-muted: #6b7280;
    --border: #374151;
    --border-strong: #4b5563;
    --accent: #3b82f6;
    --accent-light: #1e3a5f;
    --accent-text: #60a5fa;
    --success: #22c55e;
    --success-light: #052e16;
    --warning: #f59e0b;
    --warning-light: #451a03;
    --danger: #ef4444;
    --danger-light: #450a0a;
    --shadow-sm: 0 1px 2px rgba(0,0,0,0.3);
    --shadow: 0 1px 3px rgba(0,0,0,0.4), 0 1px 2px rgba(0,0,0,0.3);
  }
}

* { margin: 0; padding: 0; box-sizing: border-box; }

body {
  font-family: var(--font);
  font-size: 14px;
  color: var(--text);
  background: var(--bg);
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}

/* Layout */
.shelf-layout {
  display: flex;
  min-height: 100vh;
}

.shelf-sidebar {
  width: var(--sidebar-w);
  background: var(--bg-secondary);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  position: fixed;
  top: 0;
  left: 0;
  bottom: 0;
  z-index: 40;
}

.shelf-sidebar-header {
  height: var(--topbar-h);
  display: flex;
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid var(--border);
  gap: 10px;
}

.shelf-sidebar-logo {
  font-size: 18px;
  font-weight: 600;
  color: var(--text);
  text-decoration: none;
  display: flex;
  align-items: center;
  gap: 8px;
}

.shelf-sidebar-logo svg { color: var(--accent); }

.shelf-sidebar-version {
  font-size: 11px;
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: 2px 6px;
  border-radius: var(--radius-sm);
}

.shelf-sidebar-nav {
  flex: 1;
  overflow-y: auto;
  padding: 12px 8px;
}

.shelf-nav-section {
  margin-bottom: 20px;
}

.shelf-nav-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
  padding: 0 12px;
  margin-bottom: 4px;
}

.shelf-nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: var(--radius);
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 13px;
  font-weight: 500;
  transition: all 0.15s;
}

.shelf-nav-item:hover {
  background: var(--bg-tertiary);
  color: var(--text);
}

.shelf-nav-item.active {
  background: var(--accent-light);
  color: var(--accent-text);
}

.shelf-nav-item svg { flex-shrink: 0; opacity: 0.7; }
.shelf-nav-item.active svg { opacity: 1; }

.shelf-nav-badge {
  margin-left: auto;
  font-size: 11px;
  background: var(--bg-tertiary);
  color: var(--text-muted);
  padding: 1px 6px;
  border-radius: var(--radius-sm);
}

.shelf-sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--border);
}

.shelf-sidebar-status {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}

.shelf-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--success);
}

/* Topbar */
.shelf-topbar {
  height: var(--topbar-h);
  background: var(--bg);
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  position: fixed;
  top: 0;
  left: var(--sidebar-w);
  right: 0;
  z-index: 30;
}

.shelf-topbar-title {
  font-size: 16px;
  font-weight: 600;
}

.shelf-topbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* Main content */
.shelf-main {
  margin-left: var(--sidebar-w);
  padding-top: var(--topbar-h);
  flex: 1;
  min-height: 100vh;
}

.shelf-content {
  padding: 24px;
  max-width: 1200px;
}

/* Cards */
.shelf-card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 20px;
  box-shadow: var(--shadow-sm);
}

.shelf-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.shelf-card-title {
  font-size: 15px;
  font-weight: 600;
}

/* Stat cards */
.shelf-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.shelf-stat {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 20px;
}

.shelf-stat-label {
  font-size: 12px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 4px;
}

.shelf-stat-value {
  font-size: 28px;
  font-weight: 600;
  line-height: 1.2;
}

.shelf-stat-sub {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
}

/* Module cards */
.shelf-modules-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}

.shelf-module-card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 20px;
  transition: border-color 0.15s, box-shadow 0.15s;
  text-decoration: none;
  color: var(--text);
  display: block;
}

.shelf-module-card:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow);
}

.shelf-module-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.shelf-module-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--radius);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
}

.shelf-module-name {
  font-size: 15px;
  font-weight: 600;
}

.shelf-module-version {
  font-size: 11px;
  color: var(--text-muted);
}

.shelf-module-desc {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 12px;
  line-height: 1.5;
}

.shelf-module-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
  color: var(--text-muted);
}

.shelf-module-meta-item {
  display: flex;
  align-items: center;
  gap: 4px;
}

/* Buttons */
.shelf-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-radius: var(--radius);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border: 1px solid transparent;
  transition: all 0.15s;
  text-decoration: none;
  font-family: var(--font);
  line-height: 1;
}

.shelf-btn-primary {
  background: var(--accent);
  color: #fff;
  border-color: var(--accent);
}

.shelf-btn-primary:hover { opacity: 0.9; }

.shelf-btn-secondary {
  background: var(--bg);
  color: var(--text);
  border-color: var(--border);
}

.shelf-btn-secondary:hover {
  background: var(--bg-secondary);
  border-color: var(--border-strong);
}

.shelf-btn-ghost {
  background: transparent;
  color: var(--text-secondary);
  padding: 6px 8px;
}

.shelf-btn-ghost:hover {
  background: var(--bg-tertiary);
  color: var(--text);
}

.shelf-btn-sm { padding: 4px 10px; font-size: 12px; }
.shelf-btn-icon { padding: 6px; }

/* Badges */
.shelf-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  font-weight: 500;
}

.shelf-badge-success { background: var(--success-light); color: var(--success); }
.shelf-badge-warning { background: var(--warning-light); color: var(--warning); }
.shelf-badge-danger { background: var(--danger-light); color: var(--danger); }
.shelf-badge-info { background: var(--accent-light); color: var(--accent-text); }

/* Table */
.shelf-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  font-size: 13px;
}

.shelf-table th {
  text-align: left;
  font-weight: 600;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}

.shelf-table td {
  padding: 12px;
  border-bottom: 1px solid var(--border);
  vertical-align: middle;
}

.shelf-table tr:last-child td { border-bottom: none; }

.shelf-table tr:hover td { background: var(--bg-secondary); }

/* Empty state */
.shelf-empty {
  text-align: center;
  padding: 48px 24px;
  color: var(--text-secondary);
}

.shelf-empty svg {
  margin: 0 auto 12px;
  opacity: 0.4;
  display: block;
}

.shelf-empty-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
  margin-bottom: 4px;
}

.shelf-empty-desc {
  font-size: 13px;
  margin-bottom: 16px;
}

/* Breadcrumb */
.shelf-breadcrumb {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--text-muted);
  margin-bottom: 16px;
}

.shelf-breadcrumb a {
  color: var(--text-secondary);
  text-decoration: none;
}

.shelf-breadcrumb a:hover { color: var(--text); }

/* Section header */
.shelf-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.shelf-section-title {
  font-size: 18px;
  font-weight: 600;
}

/* Util */
.shelf-flex { display: flex; }
.shelf-items-center { align-items: center; }
.shelf-gap-sm { gap: 8px; }
.shelf-gap-md { gap: 16px; }
.shelf-text-muted { color: var(--text-muted); }
.shelf-text-sm { font-size: 13px; }
.shelf-mt-lg { margin-top: 24px; }
.shelf-mb-lg { margin-bottom: 24px; }
`
