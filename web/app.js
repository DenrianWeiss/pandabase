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
    
    if (!state.activeNamespaceId) {
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
                if(confirm('Are you sure?')) {
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
            tr.innerHTML = `
                <td>
                    <div style="font-weight:500">${doc.source_uri.split('/').pop() || 'Unnamed'}</div>
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
                if(confirm('Are you sure?')) {
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
            top_k: 5
        })
    });
    
    if (res.ok) {
        const data = await res.json();
        resultsContainer.innerHTML = '';
        
        if (!data || data.length === 0) {
            resultsContainer.innerHTML = 'No results found.';
            return;
        }
        
        data.forEach(result => {
            const chunk = result.chunk || {};
            const card = document.createElement('div');
            card.className = 'glass-panel result-card';
            card.innerHTML = `
                <div class="result-meta">
                    <span>Score: ${(result.score || 0).toFixed(4)}</span>
                    <span>Doc: ${(chunk.metadata && chunk.metadata.file_name) || 'Unknown'}</span>
                </div>
                <div class="result-content">${chunk.content || ''}</div>
            `;
            resultsContainer.appendChild(card);
        });
    } else {
        resultsContainer.innerHTML = 'Search failed.';
    }
}

// Helpers
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
    const modal = document.createElement('div');
    modal.id = 'upload-modal';
    modal.className = 'modal-overlay';
    modal.innerHTML = `
        <div class="glass-card modal-content">
            <h3>Upload Document</h3>
            <div class="upload-drop-zone" id="drop-zone">
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
            <div class="upload-options">
                <label>
                    Chunk Size
                    <input type="number" id="chunk-size" value="500" min="100" max="2000">
                </label>
                <label>
                    Chunk Overlap
                    <input type="number" id="chunk-overlap" value="50" min="0" max="200">
                </label>
            </div>
            <div class="upload-progress" id="upload-progress" style="display:none;">
                <div class="progress-bar">
                    <div class="progress-fill" id="progress-fill"></div>
                </div>
                <span id="progress-text">0%</span>
            </div>
            <div class="modal-actions">
                <button class="btn-text" onclick="closeUploadModal()">Cancel</button>
                <button class="btn-primary" id="btn-confirm-upload" disabled>Upload</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);
    
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    const confirmBtn = document.getElementById('btn-confirm-upload');
    
    dropZone.addEventListener('click', () => fileInput.click());
    
    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.classList.add('drag-over');
    });
    
    dropZone.addEventListener('dragleave', () => {
        dropZone.classList.remove('drag-over');
    });
    
    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
        const files = e.dataTransfer.files;
        if (files.length > 0) {
            handleFileSelect(files[0]);
        }
    });
    
    fileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            handleFileSelect(e.target.files[0]);
        }
    });
    
    confirmBtn.addEventListener('click', performUpload);
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
                <h3>${doc.metadata?.original_filename || 'Document'}</h3>
                <button class="btn-text" onclick="closeDocumentModal()">Close</button>
            </div>
            <div class="doc-info">
                <span>ID: ${doc.id}</span>
                <span>Type: ${doc.source_type}</span>
                <span>Status: ${doc.status}</span>
                <span>Chunks: ${doc.chunks?.length || 0}</span>
                <span>Created: ${new Date(doc.created_at).toLocaleString()}</span>
            </div>
            <div class="doc-content">
                <pre>${escapeHtml(content)}</pre>
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
fetchDocuments = async function() {
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
            tr.innerHTML = `
                <td>
                    <div style="font-weight:500">${doc.source_uri.split('/').pop() || 'Unnamed'}</div>
                    <div style="font-size:0.75rem;color:var(--text-secondary)">${doc.id}</div>
                </td>
                <td>${doc.source_type}</td>
                <td>
                    <span class="status-badge status-${doc.status.toLowerCase()}">
                        ${doc.status}
                        ${isProcessing ? '<span class="spinner"></span>' : ''}
                    </span>
                </td>
                <td>
                    <button class="btn-text btn-view" data-id="${doc.id}" ${isProcessing ? 'disabled' : ''}>View</button>
                    <button class="btn-text btn-download" data-id="${doc.id}" ${isProcessing ? 'disabled' : ''}>Download</button>
                    <button class="btn-text btn-update" data-id="${doc.id}" ${isProcessing ? 'disabled' : ''}>Update</button>
                    <button class="btn-text btn-delete" data-id="${doc.id}">Delete</button>
                </td>
            `;
            tbody.appendChild(tr);
        });
        
        // Attach event listeners
        document.querySelectorAll('.btn-delete').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                if(confirm('Are you sure?')) {
                    await fetchWithAuth(`/api/v1/namespaces/${state.activeNamespaceId}/documents/${e.target.dataset.id}`, { method: 'DELETE' });
                    fetchDocuments();
                }
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
    }
};
