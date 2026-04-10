// Basic Application State
let state = {
    token: localStorage.getItem('token') || null,
    user: null,
    namespaces: [],
    activeNamespaceId: null,
    currentView: 'docs', // 'docs' or 'search'
    theme: localStorage.getItem('theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
};

// DOM Elements
const appDiv = document.getElementById('app');

// Initialize
async function init() {
    initTheme();
    if (state.token) {
        fetchUser();
    } else {
        try {
            const res = await fetch('/api/v1/auth/status');
            const data = await res.json();
            if (data.initialized) {
                renderLogin();
            } else {
                renderRegister();
            }
        } catch (e) {
            renderLogin();
        }
    }
}

// Render Register Page
function renderRegister() {
    const tpl = document.getElementById('tpl-register').content.cloneNode(true);
    appDiv.innerHTML = '';
    appDiv.appendChild(tpl);

    // Theme toggle not strictly needed in register but good for consistency
    renderThemeToggle('theme-toggle-register');

    document.getElementById('register-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        const name = document.getElementById('reg-name').value;
        const email = document.getElementById('reg-email').value;
        const password = document.getElementById('reg-password').value;
        const errDiv = document.getElementById('register-error');

        try {
            const res = await fetch('/api/v1/auth/register', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, email, password })
            });
            const data = await res.json();

            if (res.ok) {
                state.token = data.access_token;
                localStorage.setItem('token', state.token);
                fetchUser();
            } else {
                errDiv.textContent = data.error || 'Registration failed';
            }
        } catch (err) {
            errDiv.textContent = 'Network error';
        }
    });
}

// Render Login Page
function renderLogin() {
    const tpl = document.getElementById('tpl-login').content.cloneNode(true);
    appDiv.innerHTML = '';
    appDiv.appendChild(tpl);

    renderThemeToggle('theme-toggle-login');

    document.getElementById('login-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        const email = document.getElementById('email').value;
        const password = document.getElementById('password').value;
        const errDiv = document.getElementById('login-error');

        try {
            const res = await fetch('/api/v1/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, password })
            });
            const data = await res.json();

            if (res.ok) {
                state.token = data.access_token;
                localStorage.setItem('token', state.token);
                fetchUser();
            } else {
                errDiv.textContent = data.error || 'Login failed';
            }
        } catch (err) {
            errDiv.textContent = 'Network error';
        }
    });

    document.getElementById('btn-oauth-github').addEventListener('click', () => {
        window.location.href = '/api/v1/oauth/github';
    });
}

// Fetch user data
async function fetchUser() {
    try {
        const res = await fetchWithAuth('/api/v1/auth/me');
        if (res.ok) {
            state.user = await res.json();
            renderDashboard();
            fetchNamespaces();
        } else {
            logout();
        }
    } catch {
        logout();
    }
}

// Render Dashboard Shell
function renderDashboard() {
    const tpl = document.getElementById('tpl-dashboard').content.cloneNode(true);
    appDiv.innerHTML = '';
    appDiv.appendChild(tpl);

    document.getElementById('user-name').textContent = state.user.name;
    document.getElementById('btn-logout').addEventListener('click', logout);

    if (state.user.role === 'admin') {
        const navUsers = document.getElementById('nav-users');
        if (navUsers) navUsers.style.display = 'block';
    }

    // Sidebar navigation
    document.querySelectorAll('.nav-links li').forEach(li => {
        li.addEventListener('click', (e) => {
            document.querySelectorAll('.nav-links li').forEach(el => el.classList.remove('active'));
            e.currentTarget.classList.add('active');
            state.currentView = e.currentTarget.dataset.view;
            renderView();
        });
    });

    // Namespace Selector changes
    document.getElementById('ns-select').addEventListener('change', (e) => {
        state.activeNamespaceId = e.target.value;
        renderView();
    });

    renderThemeToggle('theme-toggle-dashboard');

    document.getElementById('btn-new-ns').addEventListener('click', createNamespace);
}

// Fetch Namespaces
async function fetchNamespaces() {
    const res = await fetchWithAuth('/api/v1/namespaces');
    if (res.ok) {
        state.namespaces = await res.json();
        const select = document.getElementById('ns-select');
        select.innerHTML = '';
        if (state.namespaces.length === 0) {
            const opt = document.createElement('option');
            opt.textContent = 'No Workspaces';
            select.appendChild(opt);
        } else {
            state.namespaces.forEach(ns => {
                const opt = document.createElement('option');
                opt.value = ns.id;
                opt.textContent = ns.name;
                select.appendChild(opt);
            });
            state.activeNamespaceId = state.namespaces[0].id;
            renderView();
        }
    }
}

async function createNamespace() {
    const name = prompt("Enter Workspace Name:");
    if (!name) return;
    const res = await fetchWithAuth('/api/v1/namespaces', {
        method: 'POST',
        body: JSON.stringify({ name, description: '' })
    });
    if (res.ok) {
        fetchNamespaces();
    } else {
        alert("Failed to create workspace");
    }
}

// Render Inner View (Docs or Search)
function renderView() {
    const container = document.getElementById('view-container');
    container.innerHTML = '';

    if (!state.activeNamespaceId && (state.currentView === 'docs' || state.currentView === 'search')) {
        container.innerHTML = '<p style="color:var(--text-secondary)">Please select or create a workspace first.</p>';
        return;
    }

    if (state.currentView === 'docs') {
        const tpl = document.getElementById('tpl-view-docs').content.cloneNode(true);
        container.appendChild(tpl);
        fetchDocuments();

        document.getElementById('btn-upload').addEventListener('click', showUploadModal);
    } else if (state.currentView === 'search') {
        const tpl = document.getElementById('tpl-view-search').content.cloneNode(true);
        container.appendChild(tpl);

        document.getElementById('btn-do-search').addEventListener('click', performSearch);
        document.getElementById('search-input').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') performSearch();
        });
    } else if (state.currentView === 'users') {
        const tpl = document.getElementById('tpl-view-users').content.cloneNode(true);
        container.appendChild(tpl);
        fetchUsers();

        document.getElementById('btn-add-user').addEventListener('click', addUser);
    } else if (state.currentView === 'security') {
        const tpl = document.getElementById('tpl-view-security').content.cloneNode(true);
        container.appendChild(tpl);
        fetchAPITokens();

        document.getElementById('btn-create-token').addEventListener('click', createAPIToken);
        document.getElementById('password-reset-form').addEventListener('submit', changePassword);
    }
}

async function addUser() {
    const name = prompt("Enter Name:");
    if (!name) return;
    const email = prompt("Enter Email:");
    if (!email) return;
    const password = prompt("Enter Password (min 8 chars):");
    if (!password || password.length < 8) {
        alert("Password too short");
        return;
    }

    const res = await fetchWithAuth('/api/v1/users', {
        method: 'POST',
        body: JSON.stringify({ name, email, password })
    });

    if (res.ok) {
        alert("User added successfully");
        fetchUsers();
    } else {
        const data = await res.json();
        alert("Failed to add user: " + (data.error || 'Unknown error'));
    }
}

async function fetchUsers() {
    const tbody = document.getElementById('user-list');
    tbody.innerHTML = '<tr><td colspan="3">Loading...</td></tr>';

    const res = await fetchWithAuth(`/api/v1/users`);
    if (res.ok) {
        const users = await res.json();
        tbody.innerHTML = '';
        if (users.length === 0) {
            tbody.innerHTML = '<tr><td colspan="3">No users found.</td></tr>';
            return;
        }

        users.forEach(user => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>
                    <div style="font-weight:500">${user.name}</div>
                    <div style="font-size:0.75rem;color:var(--text-secondary)">${user.email}</div>
                </td>
                <td>${user.role}</td>
                <td>
                    <button class="btn-text btn-role" data-id="${user.id}" data-role="${user.role}">Change Role</button>
                    <button class="btn-text btn-delete-user" data-id="${user.id}" ${user.id === state.user.id ? 'disabled' : ''}>Delete</button>
                </td>
            `;
            tbody.appendChild(tr);
        });

        document.querySelectorAll('.btn-delete-user').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                if (confirm('Are you sure?')) {
                    await fetchWithAuth(`/api/v1/users/${e.target.dataset.id}`, { method: 'DELETE' });
                    fetchUsers();
                }
            });
        });

        document.querySelectorAll('.btn-role').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const newRole = prompt("Enter new role (admin, user, viewer):", e.target.dataset.role);
                if (newRole && newRole !== e.target.dataset.role) {
                    await fetchWithAuth(`/api/v1/users/${e.target.dataset.id}`, {
                        method: 'PUT',
                        body: JSON.stringify({ role: newRole })
                    });
                    fetchUsers();
                }
            });
        });
    }
}

async function fetchAPITokens() {
    const tbody = document.getElementById('token-list');
    if (!tbody) return;

    tbody.innerHTML = '<tr><td colspan="6">Loading...</td></tr>';
    const res = await fetchWithAuth('/api/v1/auth/tokens');
    if (!res.ok) {
        tbody.innerHTML = '<tr><td colspan="6">Failed to load tokens.</td></tr>';
        return;
    }

    const tokens = await res.json();
    tbody.innerHTML = '';
    if (!tokens || tokens.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6">No tokens created yet.</td></tr>';
        return;
    }

    tokens.forEach(token => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
            <td>${token.name}</td>
            <td>${token.prefix}</td>
            <td>${new Date(token.created_at).toLocaleString()}</td>
            <td>${token.last_used_at ? new Date(token.last_used_at).toLocaleString() : '-'}</td>
            <td>${token.expires_at ? new Date(token.expires_at).toLocaleString() : 'Never'}</td>
            <td><button class="btn-text btn-delete-token" data-id="${token.id}">Delete</button></td>
        `;
        tbody.appendChild(tr);
    });

    document.querySelectorAll('.btn-delete-token').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            if (!confirm('Delete this token?')) return;
            const tokenId = e.target.dataset.id;
            const del = await fetchWithAuth(`/api/v1/auth/tokens/${tokenId}`, { method: 'DELETE' });
            if (del.ok) {
                fetchAPITokens();
            } else {
                alert('Failed to delete token');
            }
        });
    });
}

async function createAPIToken() {
    const nameInput = document.getElementById('token-name');
    const expiryInput = document.getElementById('token-expiry-days');
    const resultEl = document.getElementById('token-create-result');
    if (!nameInput || !resultEl) return;

    const name = nameInput.value.trim();
    if (!name) {
        alert('Token name is required');
        return;
    }

    const payload = {
        name,
        expires_in_days: expiryInput && expiryInput.value ? parseInt(expiryInput.value, 10) : 0,
    };

    const res = await fetchWithAuth('/api/v1/auth/tokens', {
        method: 'POST',
        body: JSON.stringify(payload),
    });

    if (!res.ok) {
        const data = await res.json();
        resultEl.textContent = `Create failed: ${data.error || 'unknown error'}`;
        return;
    }

    const data = await res.json();
    resultEl.textContent = `Created token ${data.token.name}. Copy now: ${data.plain_text}`;
    nameInput.value = '';
    if (expiryInput) expiryInput.value = '';
    fetchAPITokens();
}

async function changePassword(e) {
    e.preventDefault();
    const currentPassword = document.getElementById('current-password').value;
    const newPassword = document.getElementById('new-password').value;
    const msg = document.getElementById('password-reset-msg');

    const res = await fetchWithAuth('/api/v1/auth/change-password', {
        method: 'POST',
        body: JSON.stringify({
            current_password: currentPassword,
            new_password: newPassword,
        }),
    });

    if (res.ok) {
        msg.textContent = 'Password updated successfully.';
        document.getElementById('password-reset-form').reset();
        return;
    }

    const data = await res.json();
    msg.textContent = `Password update failed: ${data.error || 'unknown error'}`;
}

async function fetchDocuments() {
    const tbody = document.getElementById('doc-list');
    tbody.innerHTML = '<tr><td colspan="4">Loading...</td></tr>';

    const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents`);
    if (res.ok) {
        const data = await res.json();
        const docs = data.data || [];
        tbody.innerHTML = '';
        if (docs.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4">No documents found.</td></tr>';
            return;
        }

        docs.forEach(doc => {
            const tr = document.createElement('tr');
            // Use title from metadata if available, otherwise fallback to source_uri
            const displayName = doc.metadata?.title || doc.source_uri.split('/').pop() || 'Unnamed';
            tr.innerHTML = `
                <td>
                    <div style="font-weight:500">${escapeHtml(displayName)}</div>
                    <div style="font-size:0.75rem;color:var(--text-secondary)">${doc.id}</div>
                </td>
                <td>${doc.source_type}</td>
                <td><span class="status-badge status-${doc.status.toLowerCase()}">${doc.status}</span></td>
                <td><button class="btn-text btn-delete" data-id="${doc.id}">Delete</button></td>
            `;
            tbody.appendChild(tr);
        });

        document.querySelectorAll('.btn-delete').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                if (confirm('Are you sure?')) {
                    await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${e.target.dataset.id}`, { method: 'DELETE' });
                    fetchDocuments();
                }
            });
        });
    }
}

async function performSearch() {
    const query = document.getElementById('search-input').value;
    if (!query) return;

    const resultsContainer = document.getElementById('search-results');
    resultsContainer.innerHTML = 'Searching...';

    const res = await fetchWithAuth(`/api/v1/search`, {
        method: 'POST',
        body: JSON.stringify({
            namespace_ids: [state.activeNamespaceId],
            query: query,
            top_k: 5,
            include_content: true
        })
    });

    if (res.ok) {
        const response = await res.json();
        resultsContainer.innerHTML = '';

        // API returns { results: [...], total_count: ..., query: ..., mode: ... }
        const results = response.results || [];
        
        if (!results || results.length === 0) {
            resultsContainer.innerHTML = 'No results found.';
            return;
        }

        results.forEach(result => {
            // API returns chunk with fields: ID, Content, Metadata, etc.
            const chunk = result.chunk || {};
            const card = document.createElement('div');
            card.className = 'glass-panel result-card';
            const context = result.context || {};
            const content = chunk.content || '';
            const contextText = context.text || '';
            const hasMoreContext = !!context.truncated && !!result.document_id;
            card.innerHTML = `
                <div class="result-meta">
                    <span>Score: ${(result.final_score || result.vector_score || result.fulltext_score || 0).toFixed(4)}</span>
                    <span>Doc: ${(chunk.metadata && chunk.metadata.file_name) || 'Unknown'}</span>
                </div>
                <div class="result-content">${content}</div>
                ${contextText ? `<div class="result-context">${contextText}</div>` : ''}
                ${hasMoreContext ? `<button class="result-context-link" onclick="openChunkViewer('${result.document_id}')">View More Context</button>` : ''}
            `;
            resultsContainer.appendChild(card);
        });
    } else {
        resultsContainer.innerHTML = 'Search failed.';
    }
}

// Function to open chunk viewer in a new tab
function openChunkViewer(documentId) {
    // Open a new tab with the viewer
    const viewerUrl = `/viewer.html?docId=${encodeURIComponent(documentId)}&nsId=${encodeURIComponent(state.activeNamespaceId)}&token=${encodeURIComponent(state.token)}`;
    window.open(viewerUrl, '_blank', 'noopener,noreferrer');
}
function logout() {
    state.token = null;
    state.user = null;
    localStorage.removeItem('token');
    renderLogin();
}

function fetchWithAuth(url, options = {}) {
    options.headers = options.headers || {};
    if (!(options.body instanceof FormData)) {
        options.headers['Content-Type'] = options.headers['Content-Type'] || 'application/json';
    }
    if (state.token) {
        options.headers['Authorization'] = `Bearer ${state.token}`;
    }
    return fetch(url, options);
}

// Theme Management
function initTheme() {
    document.documentElement.setAttribute('data-theme', state.theme);
}

function renderThemeToggle(containerId) {
    const container = document.getElementById(containerId);
    if (!container) return;

    const isDark = state.theme === 'dark';
    container.innerHTML = `
        <button class="theme-switch-btn" title="Toggle theme">
            ${isDark ?
            '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>' :
            '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>'
        }
        </button>
    `;

    container.querySelector('button').addEventListener('click', () => {
        state.theme = state.theme === 'dark' ? 'light' : 'dark';
        localStorage.setItem('theme', state.theme);
        initTheme();
        // Re-render toggle to update icon
        renderThemeToggle(containerId);
    });
}

// Start
init();

// OAuth Callback Handler
function handleOAuthCallback() {
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get('token');
    const error = urlParams.get('error');

    if (error) {
        alert('OAuth login failed: ' + error);
        return;
    }

    if (token) {
        state.token = token;
        localStorage.setItem('token', token);
        // Clean up URL
        window.history.replaceState({}, document.title, window.location.pathname);
        fetchUser();
    }
}

// Check for OAuth callback on load
if (window.location.search.includes('token=') || window.location.search.includes('error=')) {
    handleOAuthCallback();
}

// Upload Modal Functions
function showUploadModal() {
    selectedFile = null;
    const modal = document.createElement('div');
    modal.id = 'upload-modal';
    modal.className = 'modal-overlay';
    modal.innerHTML = `
        <div class="glass-card modal-content">
            <div class="modal-tabs">
                <button class="tab-btn active" data-tab="file">File Upload</button>
                <button class="tab-btn" data-tab="text">Text Input</button>
                <button class="tab-btn" data-tab="url">Import from URL</button>
            </div>
            
            <div id="tab-file" class="tab-content active">
                <div class="upload-drop-zone" id="drop-zone" style="margin-top: 1rem">
                    <input type="file" id="file-input" accept=".txt,.md,.markdown,.pdf,.html,.htm" hidden>
                    <div class="upload-prompt">
                        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                            <polyline points="17 8 12 3 7 8"></polyline>
                            <line x1="12" y1="3" x2="12" y2="15"></line>
                        </svg>
                        <p>Drag & drop a file here, or click to select</p>
                        <p class="file-types">Supported: .txt, .md, .pdf, .html</p>
                    </div>
                </div>
            </div>
            
            <div id="tab-url" class="tab-content" style="display:none">
                <div class="form-group" style="margin-top: 1.5rem">
                    <label>URL</label>
                    <input type="url" id="import-url" placeholder="https://example.com or Notion URL">
                </div>
                <div class="form-group" style="margin-top: 1rem">
                    <label>Import Type</label>
                    <select id="import-type">
                        <option value="web">Webpage</option>
                        <option value="notion">Notion</option>
                    </select>
                </div>
                <div id="web-title-options" style="margin-top: 1rem; padding: 0.75rem; background: var(--bg-secondary); border-radius: 6px;">
                    <div style="display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem;">
                        <input type="checkbox" id="auto-extract-title" checked>
                        <label for="auto-extract-title" style="margin: 0; font-weight: 500;">Auto-extract title from webpage</label>
                    </div>
                    <div class="form-group" style="margin-top: 0.5rem; margin-bottom: 0;">
                        <label>Or set custom title (optional)</label>
                        <input type="text" id="custom-title" placeholder="Leave empty to auto-extract">
                    </div>
                </div>
                <div id="notion-fields" style="display:none; margin-top: 1rem">
                    <div class="form-group">
                        <label>Notion API Key</label>
                        <input type="password" id="notion-api-key" placeholder="secret_...">
                        <p style="font-size:0.75rem;color:var(--text-secondary);margin-top:0.25rem">Required for private Notion pages</p>
                    </div>
                </div>
            </div>

            <div id="tab-text" class="tab-content" style="display:none">
                <div class="form-group" style="margin-top: 1.5rem">
                    <label>Document Name</label>
                    <input type="text" id="text-doc-name" placeholder="pasted-text.txt" value="pasted-text.txt">
                </div>
                <div class="form-group" style="margin-top: 1rem">
                    <label>Text Content</label>
                    <textarea id="text-content" rows="10" placeholder="Paste text content here..."></textarea>
                </div>
            </div>

            <div class="upload-options" style="margin-top: 1.5rem">
                <div class="form-group">
                    <label>Chunk Size</label>
                    <input type="number" id="chunk-size" value="500" min="100" max="2000">
                </div>
                <div class="form-group">
                    <label>Overlap</label>
                    <input type="number" id="chunk-overlap" value="50" min="0" max="200">
                </div>
            </div>
            
            <div class="upload-progress" id="upload-progress" style="display:none; margin-top: 1rem">
                <div class="progress-bar">
                    <div class="progress-fill" id="progress-fill"></div>
                </div>
                <span id="progress-text">0%</span>
            </div>
            
            <div class="modal-actions" style="margin-top: 1.5rem">
                <button class="btn-text" onclick="closeUploadModal()">Cancel</button>
                <button class="btn-primary" id="btn-confirm-upload" disabled>Upload</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);

    // Tab switching logic
    let activeTab = 'file';
    const tabBtns = modal.querySelectorAll('.tab-btn');
    const tabContents = modal.querySelectorAll('.tab-content');
    const confirmBtn = document.getElementById('btn-confirm-upload');

    tabBtns.forEach(btn => {
        btn.addEventListener('click', () => {
            activeTab = btn.dataset.tab;
            tabBtns.forEach(b => b.classList.toggle('active', b === btn));
            tabContents.forEach(c => c.style.display = c.id === `tab-${activeTab}` ? 'block' : 'none');
            confirmBtn.textContent = activeTab === 'url' ? 'Import' : 'Upload';
            validateInputs();
        });
    });

    // File Upload Listeners
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    dropZone.addEventListener('click', () => fileInput.click());
    dropZone.addEventListener('dragover', (e) => { e.preventDefault(); dropZone.classList.add('drag-over'); });
    dropZone.addEventListener('dragleave', () => dropZone.classList.remove('drag-over'));
    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
        if (e.dataTransfer.files.length > 0) handleFileSelect(e.dataTransfer.files[0]);
    });
    fileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) handleFileSelect(e.target.files[0]);
    });

    // URL Import Listeners
    const importUrl = document.getElementById('import-url');
    const importType = document.getElementById('import-type');
    const notionFields = document.getElementById('notion-fields');
    const webTitleOptions = document.getElementById('web-title-options');
    const textContent = document.getElementById('text-content');

    importUrl.addEventListener('input', validateInputs);
    textContent.addEventListener('input', validateInputs);
    importType.addEventListener('change', () => {
        const isNotion = importType.value === 'notion';
        notionFields.style.display = isNotion ? 'block' : 'none';
        webTitleOptions.style.display = isNotion ? 'none' : 'block';
        validateInputs();
    });

    function validateInputs() {
        if (activeTab === 'file') {
            confirmBtn.disabled = !selectedFile;
        } else if (activeTab === 'text') {
            confirmBtn.disabled = !textContent.value.trim();
        } else {
            confirmBtn.disabled = !importUrl.value;
        }
    }

    confirmBtn.addEventListener('click', () => {
        if (activeTab === 'file') {
            performUpload();
        } else if (activeTab === 'text') {
            performTextUpload();
        } else {
            performUrlImport();
        }
    });
}

let selectedFile = null;

function handleFileSelect(file) {
    selectedFile = file;
    const dropZone = document.getElementById('drop-zone');
    const confirmBtn = document.getElementById('btn-confirm-upload');

    dropZone.innerHTML = `
        <div class="file-selected">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                <polyline points="14 2 14 8 20 8"></polyline>
            </svg>
            <div class="file-info">
                <span class="file-name">${file.name}</span>
                <span class="file-size">${formatFileSize(file.size)}</span>
            </div>
            <button class="btn-text" onclick="event.stopPropagation(); resetFileSelection()">Change</button>
        </div>
    `;
    confirmBtn.disabled = false;
}

function resetFileSelection() {
    selectedFile = null;
    const dropZone = document.getElementById('drop-zone');
    const confirmBtn = document.getElementById('btn-confirm-upload');

    dropZone.innerHTML = `
        <input type="file" id="file-input" accept=".txt,.md,.markdown,.pdf,.html,.htm" hidden>
        <div class="upload-prompt">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                <polyline points="17 8 12 3 7 8"></polyline>
                <line x1="12" y1="3" x2="12" y2="15"></line>
            </svg>
            <p>Drag & drop a file here, or click to select</p>
            <p class="file-types">Supported: .txt, .md, .pdf, .html</p>
        </div>
    `;
    confirmBtn.disabled = true;

    // Re-attach event listeners
    const fileInput = document.getElementById('file-input');
    dropZone.addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            handleFileSelect(e.target.files[0]);
        }
    });
}

function formatFileSize(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

async function performUrlImport() {
    const url = document.getElementById('import-url').value;
    const type = document.getElementById('import-type').value;
    const notionKey = document.getElementById('notion-api-key').value;
    const chunkSize = document.getElementById('chunk-size').value;
    const chunkOverlap = document.getElementById('chunk-overlap').value;
    const autoExtractTitle = document.getElementById('auto-extract-title').checked;
    const customTitle = document.getElementById('custom-title').value.trim();

    if (!url) return;

    const progressDiv = document.getElementById('upload-progress');
    const progressFill = document.getElementById('progress-fill');
    const progressText = document.getElementById('progress-text');
    const confirmBtn = document.getElementById('btn-confirm-upload');

    progressDiv.style.display = 'flex';
    confirmBtn.disabled = true;

    const payload = {
        url: url,
        parser_type: type,
        notion_api_key: notionKey,
        render_javascript: type === 'web',
        render_timeout: type === 'web' ? 20 : 0,
        render_fallback: false,
        chunk_size: parseInt(chunkSize),
        chunk_overlap: parseInt(chunkOverlap),
        auto_extract_title: autoExtractTitle
    };

    // Add custom title if provided
    if (customTitle) {
        payload.title = customTitle;
    }

    try {
        let progress = 0;
        const progressInterval = setInterval(() => {
            progress += Math.random() * 20;
            if (progress > 95) progress = 95;
            progressFill.style.width = progress + '%';
            progressText.textContent = Math.round(progress) + '%';
        }, 150);

        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/import`, {
            method: 'POST',
            body: JSON.stringify(payload),
            headers: { 'Content-Type': 'application/json' }
        });

        clearInterval(progressInterval);

        if (res.ok) {
            progressFill.style.width = '100%';
            progressText.textContent = '100%';
            setTimeout(() => {
                closeUploadModal();
                fetchDocuments();
                startDocumentStatusPolling();
            }, 500);
        } else {
            const data = await res.json();
            alert('Import failed: ' + (data.error || 'Unknown error'));
            progressDiv.style.display = 'none';
            confirmBtn.disabled = false;
        }
    } catch (err) {
        alert('Import failed: ' + err.message);
        progressDiv.style.display = 'none';
        confirmBtn.disabled = false;
    }
}

async function performTextUpload() {
    const textArea = document.getElementById('text-content');
    const fileNameInput = document.getElementById('text-doc-name');
    const chunkSize = document.getElementById('chunk-size').value;
    const chunkOverlap = document.getElementById('chunk-overlap').value;

    const text = textArea.value.trim();
    if (!text) return;

    let fileName = (fileNameInput.value || '').trim();
    if (!fileName) {
        fileName = 'pasted-text.txt';
    }
    if (!fileName.toLowerCase().endsWith('.txt')) {
        fileName += '.txt';
    }

    const progressDiv = document.getElementById('upload-progress');
    const progressFill = document.getElementById('progress-fill');
    const progressText = document.getElementById('progress-text');
    const confirmBtn = document.getElementById('btn-confirm-upload');

    progressDiv.style.display = 'flex';
    confirmBtn.disabled = true;

    const textBlob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    const formData = new FormData();
    formData.append('file', textBlob, fileName);
    formData.append('chunk_size', chunkSize);
    formData.append('chunk_overlap', chunkOverlap);
    formData.append('parser_type', 'text');

    try {
        let progress = 0;
        const progressInterval = setInterval(() => {
            progress += Math.random() * 15;
            if (progress > 90) progress = 90;
            progressFill.style.width = progress + '%';
            progressText.textContent = Math.round(progress) + '%';
        }, 200);

        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents`, {
            method: 'POST',
            body: formData,
            headers: {}
        });

        clearInterval(progressInterval);

        if (res.ok) {
            progressFill.style.width = '100%';
            progressText.textContent = '100%';
            setTimeout(() => {
                closeUploadModal();
                fetchDocuments();
                startDocumentStatusPolling();
            }, 500);
        } else {
            const data = await res.json();
            alert('Upload failed: ' + (data.error || 'Unknown error'));
            progressDiv.style.display = 'none';
            confirmBtn.disabled = false;
        }
    } catch (err) {
        alert('Upload failed: ' + err.message);
        progressDiv.style.display = 'none';
        confirmBtn.disabled = false;
    }
}

async function performUpload() {
    if (!selectedFile) return;

    const progressDiv = document.getElementById('upload-progress');
    const progressFill = document.getElementById('progress-fill');
    const progressText = document.getElementById('progress-text');
    const confirmBtn = document.getElementById('btn-confirm-upload');

    progressDiv.style.display = 'flex';
    confirmBtn.disabled = true;

    const chunkSize = document.getElementById('chunk-size').value;
    const chunkOverlap = document.getElementById('chunk-overlap').value;

    const formData = new FormData();
    formData.append('file', selectedFile);
    formData.append('chunk_size', chunkSize);
    formData.append('chunk_overlap', chunkOverlap);

    try {
        // Simulate progress since fetch doesn't support upload progress natively
        let progress = 0;
        const progressInterval = setInterval(() => {
            progress += Math.random() * 15;
            if (progress > 90) progress = 90;
            progressFill.style.width = progress + '%';
            progressText.textContent = Math.round(progress) + '%';
        }, 200);

        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents`, {
            method: 'POST',
            body: formData,
            headers: {} // Let browser set Content-Type with boundary
        });

        clearInterval(progressInterval);

        if (res.ok) {
            progressFill.style.width = '100%';
            progressText.textContent = '100%';
            setTimeout(() => {
                closeUploadModal();
                fetchDocuments();
                // Start polling for document status
                startDocumentStatusPolling();
            }, 500);
        } else {
            const data = await res.json();
            alert('Upload failed: ' + (data.error || 'Unknown error'));
            progressDiv.style.display = 'none';
            confirmBtn.disabled = false;
        }
    } catch (err) {
        alert('Upload failed: ' + err.message);
        progressDiv.style.display = 'none';
        confirmBtn.disabled = false;
    }
}

function closeUploadModal() {
    const modal = document.getElementById('upload-modal');
    if (modal) {
        modal.remove();
    }
    selectedFile = null;
}

// Document View Modal
async function showDocumentModal(docId) {
    // Fetch document details
    const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${docId}`);
    if (!res.ok) {
        alert('Failed to load document');
        return;
    }
    const doc = await res.json();

    // Fetch file content
    const contentRes = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${docId}/download`);
    let content = '';
    if (contentRes.ok) {
        content = await contentRes.text();
    } else {
        content = 'Failed to load content';
    }

    const modal = document.createElement('div');
    modal.id = 'doc-view-modal';
    modal.className = 'modal-overlay';
    modal.innerHTML = `
        <div class="glass-card modal-content modal-large">
            <div class="modal-header">
                <h3>${doc.metadata?.title || doc.metadata?.original_filename || 'Document'}</h3>
                <button class="btn-text" onclick="closeDocumentModal()">Close</button>
            </div>
            <div class="doc-info">
                <span>ID: ${doc.id}</span>
                <span>Type: ${doc.source_type}</span>
                <span class="status-badge status-${doc.status.toLowerCase()}">${doc.status}</span>
                <span>Chunks: ${doc.chunks?.length || 0}</span>
                <span>Created: ${new Date(doc.created_at).toLocaleString()}</span>
                ${doc.error_message ? `<div style="width:100%;color:var(--danger);margin-top:0.5rem"><strong>Error:</strong> ${escapeHtml(doc.error_message)}</div>` : ''}
            </div>
            <div class="doc-content">
                <pre>${escapeHtml(content)}</pre>
            </div>
            <div class="modal-actions" style="margin-top: 1rem">
                ${doc.status === 'failed' ? `<button class="btn-primary" onclick="retryDocument('${doc.id}'); closeDocumentModal();">Retry Processing</button>` : ''}
                <button class="btn-text" onclick="closeDocumentModal()">Close</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);
}

function closeDocumentModal() {
    const modal = document.getElementById('doc-view-modal');
    if (modal) {
        modal.remove();
    }
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Document Update Modal
let updateDocId = null;
let updateFile = null;


async function retryDocument(docId) {
    try {
        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${docId}/retry`, {
            method: 'POST'
        });

        if (res.ok) {
            fetchDocuments();
            startDocumentStatusPolling();
        } else {
            const data = await res.json();
            alert('Retry failed: ' + (data.error || 'Unknown error'));
        }
    } catch (err) {
        alert('Retry failed: ' + err.message);
    }
}

function showUpdateModal(docId) {
    updateDocId = docId;
    updateFile = null;

    const modal = document.createElement('div');
    modal.id = 'update-modal';
    modal.className = 'modal-overlay';
    modal.innerHTML = `
        <div class="glass-card modal-content">
            <h3>Update Document</h3>
            <p>Upload a new version of this document. The system will compare chunks and only re-embed changed content.</p>
            <div class="upload-drop-zone" id="update-drop-zone">
                <input type="file" id="update-file-input" hidden>
                <div class="upload-prompt">
                    <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                        <polyline points="17 8 12 3 7 8"></polyline>
                        <line x1="12" y1="3" x2="12" y2="15"></line>
                    </svg>
                    <p>Click to select new file version</p>
                </div>
            </div>
            <div id="update-file-info" style="display:none; margin: 1rem 0;">
                <span id="update-filename"></span>
                <button class="btn-text" onclick="resetUpdateFile()">Change</button>
            </div>
            <div class="upload-options">
                <label>
                    Chunk Size
                    <input type="number" id="update-chunk-size" value="500" min="100" max="2000">
                </label>
                <label>
                    Chunk Overlap
                    <input type="number" id="update-chunk-overlap" value="50" min="0" max="200">
                </label>
            </div>
            <div class="upload-progress" id="update-progress" style="display:none;">
                <div class="progress-bar">
                    <div class="progress-fill" id="update-progress-fill"></div>
                </div>
                <span id="update-progress-text">0%</span>
            </div>
            <div class="modal-actions">
                <button class="btn-text" onclick="closeUpdateModal()">Cancel</button>
                <button class="btn-primary" id="btn-confirm-update" disabled>Update</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);

    const dropZone = document.getElementById('update-drop-zone');
    const fileInput = document.getElementById('update-file-input');
    const confirmBtn = document.getElementById('btn-confirm-update');

    dropZone.addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            handleUpdateFileSelect(e.target.files[0]);
        }
    });
    confirmBtn.addEventListener('click', performUpdate);
}

function handleUpdateFileSelect(file) {
    updateFile = file;
    document.getElementById('update-drop-zone').style.display = 'none';
    document.getElementById('update-file-info').style.display = 'block';
    document.getElementById('update-filename').textContent = `${file.name} (${formatFileSize(file.size)})`;
    document.getElementById('btn-confirm-update').disabled = false;
}

function resetUpdateFile() {
    updateFile = null;
    document.getElementById('update-drop-zone').style.display = 'block';
    document.getElementById('update-file-info').style.display = 'none';
    document.getElementById('update-file-input').value = '';
    document.getElementById('btn-confirm-update').disabled = true;
}

function closeUpdateModal() {
    const modal = document.getElementById('update-modal');
    if (modal) {
        modal.remove();
    }
    updateDocId = null;
    updateFile = null;
}

async function performUpdate() {
    if (!updateFile || !updateDocId) return;

    const progressDiv = document.getElementById('update-progress');
    const progressFill = document.getElementById('update-progress-fill');
    const progressText = document.getElementById('update-progress-text');
    const confirmBtn = document.getElementById('btn-confirm-update');

    progressDiv.style.display = 'flex';
    confirmBtn.disabled = true;

    const chunkSize = document.getElementById('update-chunk-size').value;
    const chunkOverlap = document.getElementById('update-chunk-overlap').value;

    const formData = new FormData();
    formData.append('file', updateFile);
    formData.append('chunk_size', chunkSize);
    formData.append('chunk_overlap', chunkOverlap);

    try {
        // Simulate progress
        let progress = 0;
        const progressInterval = setInterval(() => {
            progress += Math.random() * 15;
            if (progress > 90) progress = 90;
            progressFill.style.width = progress + '%';
            progressText.textContent = Math.round(progress) + '%';
        }, 200);

        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${updateDocId}`, {
            method: 'PUT',
            body: formData,
            headers: {}
        });

        clearInterval(progressInterval);

        if (res.ok) {
            progressFill.style.width = '100%';
            progressText.textContent = '100%';
            setTimeout(() => {
                closeUpdateModal();
                fetchDocuments();
                startDocumentStatusPolling();
            }, 500);
        } else {
            const data = await res.json();
            alert('Update failed: ' + (data.error || 'Unknown error'));
            progressDiv.style.display = 'none';
            confirmBtn.disabled = false;
        }
    } catch (err) {
        alert('Update failed: ' + err.message);
        progressDiv.style.display = 'none';
        confirmBtn.disabled = false;
    }
}

// Document Status Polling
let statusPollingInterval = null;

function startDocumentStatusPolling() {
    // Clear existing interval
    if (statusPollingInterval) {
        clearInterval(statusPollingInterval);
    }

    // Poll every 3 seconds for 2 minutes
    let pollCount = 0;
    const maxPolls = 40;

    statusPollingInterval = setInterval(async () => {
        pollCount++;
        if (pollCount > maxPolls) {
            clearInterval(statusPollingInterval);
            return;
        }

        // Only refresh if we're on the docs view
        if (state.currentView === 'docs') {
            await fetchDocuments();
        }

        // Check if all documents are completed/failed
        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents`);
        if (res.ok) {
            const data = await res.json();
            const docs = data.data || [];
            const hasPending = docs.some(doc => doc.status === 'pending' || doc.status === 'processing');

            if (!hasPending) {
                clearInterval(statusPollingInterval);
            }
        }
    }, 3000);
}

// Update fetchDocuments to show status indicators
const originalFetchDocuments = fetchDocuments;
fetchDocuments = async function () {
    const tbody = document.getElementById('doc-list');
    if (!tbody) return;

    tbody.innerHTML = '<tr><td colspan="4">Loading...</td></tr>';

    const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents`);
    if (res.ok) {
        const data = await res.json();
        const docs = data.data || [];
        tbody.innerHTML = '';

        if (docs.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4">No documents found.</td></tr>';
            return;
        }

        docs.forEach(doc => {
            const tr = document.createElement('tr');
            const isProcessing = doc.status === 'pending' || doc.status === 'processing';
            // Use title from metadata if available
            const displayName = doc.metadata?.title || doc.source_uri.split('/').pop() || 'Unnamed';
            tr.innerHTML = `
                <td>
                    <div style="font-weight:500">${escapeHtml(displayName)}</div>
                    <div style="font-size:0.75rem;color:var(--text-secondary)">${doc.id}</div>
                </td>
                <td>${doc.source_type}</td>
                <td>
                    <span class="status-badge status-${doc.status.toLowerCase()}" ${doc.error_message ? `title="${escapeHtml(doc.error_message)}"` : ''}>
                        ${doc.status}
                        ${isProcessing ? '<span class="spinner"></span>' : ''}
                        ${doc.error_message ? '<span style="margin-left:4px;cursor:help">ⓘ</span>' : ''}
                    </span>
                </td>
                <td>
                    <button class="btn-text btn-view" data-id="${doc.id}" ${isProcessing ? 'disabled' : ''}>View</button>
                    ${doc.status.toLowerCase() === 'failed' ? `<button class="btn-text btn-retry" data-id="${doc.id}">Retry</button>` : ''}
                    <button class="btn-text btn-download" data-id="${doc.id}" ${isProcessing ? 'disabled' : ''}>Download</button>
                    <button class="btn-text btn-update" data-id="${doc.id}" ${isProcessing ? 'disabled' : ''}>Update</button>
                    ${doc.source_type === 'web' || doc.source_type === 'notion' ? `<button class="btn-text btn-edit-title" data-id="${doc.id}" data-title="${escapeHtml(doc.metadata?.title || '')}">Edit Title</button>` : ''}
                    <button class="btn-text btn-delete" data-id="${doc.id}">Delete</button>
                </td>
            `;
            tbody.appendChild(tr);
        });

        // Attach event listeners
        document.querySelectorAll('.btn-delete').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                if (confirm('Are you sure?')) {
                    await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${e.target.dataset.id}`, { method: 'DELETE' });
                    fetchDocuments();
                }
            });
        });

        document.querySelectorAll('.btn-retry').forEach(btn => {
            btn.addEventListener('click', () => {
                const id = btn.dataset.id;
                retryDocument(id);
            });
        });

        document.querySelectorAll('.btn-view').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const docId = e.target.dataset.id;
                showDocumentModal(docId);
            });
        });

        document.querySelectorAll('.btn-update').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const docId = e.target.dataset.id;
                showUpdateModal(docId);
            });
        });

        document.querySelectorAll('.btn-download').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const docId = e.target.dataset.id;
                window.open(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${docId}/download`, '_blank');
            });
        });

        // Add edit title button listeners
        document.querySelectorAll('.btn-edit-title').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const docId = e.target.dataset.id;
                const currentTitle = e.target.dataset.title;
                const newTitle = prompt('Enter new title:', currentTitle);
                if (newTitle !== null && newTitle !== currentTitle) {
                    await updateDocumentTitle(docId, newTitle);
                }
            });
        });
    }
};

// Function to update document title
async function updateDocumentTitle(docId, title) {
    try {
        const res = await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${docId}/title`, {
            method: 'PATCH',
            body: JSON.stringify({ title })
        });

        if (res.ok) {
            fetchDocuments();
        } else {
            const data = await res.json();
            alert('Failed to update title: ' + (data.error || 'Unknown error'));
        }
    } catch (err) {
        alert('Failed to update title: ' + err.message);
    }
}
