// App Router and Logic
document.addEventListener('DOMContentLoaded', () => {
    initNavigation();
    loadProfile();
    loadData();
    setInterval(loadData, 5000); // Poll every 5 seconds for live feel
});

function initNavigation() {
    const navItems = document.querySelectorAll('.nav-item');
    navItems.forEach(item => {
        item.addEventListener('click', (e) => {
            e.preventDefault();
            const tabId = item.getAttribute('data-tab');
            
            // Switch UI active states
            document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
            document.querySelectorAll('.tab-pane').forEach(t => t.classList.remove('active'));
            
            item.classList.add('active');
            document.getElementById(`tab-${tabId}`).classList.add('active');
            
            // Set header title
            document.getElementById('pageTitle').innerText = item.innerText;
        });
    });

    document.getElementById('settingsForm').addEventListener('submit', saveSettings);
    
    document.getElementById('forceSyncBtn').addEventListener('click', () => {
        fetch('/api/sync/all', { method: 'POST' }).then(() => {
            alert('Full sync triggered! Check logs.');
        });
    });
}

async function loadData() {
    // Workspaces
    const wsRes = await fetch('/api/workspaces').then(r => r.json());
    renderWorkspaces(wsRes);

    // Repositories
    const repoRes = await fetch('/api/repositories').then(r => r.json());
    renderRepositories(repoRes);

    // Pending Deletions
    const delRes = await fetch('/api/pending-deletions').then(r => r.json());
    renderDeletions(delRes);

    // Logs
    const logRes = await fetch('/api/logs').then(r => r.json());
    renderLogs(logRes);
}

async function loadProfile() {
    const settings = await fetch('/api/settings').then(r => r.json());
    if (settings.github_token) {
        document.getElementById('ghToken').value = settings.github_token;
    }
    if (settings.openai_key) document.getElementById('oaKey').value = settings.openai_key;
    if (settings.gemini_key) document.getElementById('gemKey').value = settings.gemini_key;

    if (settings.github_profile) {
        document.getElementById('ghUsername').innerText = settings.github_profile.login || 'Unknown User';
        if (settings.github_profile.avatar_url) {
            document.getElementById('ghAvatar').src = settings.github_profile.avatar_url;
        }
    }
}

async function saveSettings(e) {
    e.preventDefault();
    const btn = document.getElementById('saveSettingsBtn');
    btn.innerText = 'Saving...';
    
    const payload = {
        github_token: document.getElementById('ghToken').value,
        openai_key: document.getElementById('oaKey').value,
        gemini_key: document.getElementById('gemKey').value
    };

    const res = await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    }).then(r => r.json());

    if (res.profile) {
        document.getElementById('ghUsername').innerText = res.profile.login;
        document.getElementById('ghAvatar').src = res.profile.avatar_url;
    }
    
    btn.innerText = 'Saved!';
    setTimeout(() => { btn.innerText = 'Save Settings'; }, 2000);
}

function renderWorkspaces(workspaces) {
    const grid = document.getElementById('workspacesGrid');
    if (!workspaces || workspaces.length === 0) {
        grid.innerHTML = '<div style="color:var(--text-secondary)">No workspaces added yet. Add one to start monitoring.</div>';
        return;
    }

    grid.innerHTML = workspaces.map(ws => `
        <div class="glass-panel">
            <h4 style="margin-bottom:8px; word-break:break-all;">${ws.path}</h4>
            <div style="display:flex; justify-content:space-between; margin-bottom: 16px;">
                <span class="badge ${ws.visibility.toLowerCase()}">${ws.visibility}</span>
                <span class="badge" style="background:${ws.is_paused ? 'rgba(239,68,68,0.1)' : 'rgba(16,185,129,0.1)'}; color:${ws.is_paused ? 'var(--accent-red)' : 'var(--accent-green)'}">${ws.is_paused ? 'PAUSED' : 'WATCHING'}</span>
            </div>
            <div style="display:flex; gap:8px;">
                <button class="btn btn-secondary" onclick="toggleWorkspace('${ws.path.replace(/\\/g, '\\\\')}', ${ws.is_paused})">
                    ${ws.is_paused ? 'Resume' : 'Pause'}
                </button>
                <button class="btn btn-danger" onclick="removeWorkspace('${ws.path.replace(/\\/g, '\\\\')}')">Remove</button>
            </div>
        </div>
    `).join('');
}

async function addWorkspace() {
    const path = document.getElementById('newWsPath').value;
    const vis = document.getElementById('newWsVis').value;
    if (!path) return;

    const res = await fetch('/api/add-workspace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path, visibility: vis })
    });
    
    if (res.ok) {
        document.getElementById('addWorkspaceModal').classList.remove('active');
        document.getElementById('newWsPath').value = '';
        loadData();
    } else {
        const error = await res.json();
        alert('Failed: ' + error.detail);
    }
}

async function removeWorkspace(path) {
    if(!confirm('Are you sure you want to stop monitoring this workspace?')) return;
    await fetch('/api/remove-workspace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path })
    });
    loadData();
}

async function toggleWorkspace(path, is_paused) {
    const endpoint = is_paused ? '/api/resume-workspace' : '/api/pause-workspace';
    await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path })
    });
    loadData();
}

function renderRepositories(repos) {
    const list = document.getElementById('repositoriesList');
    if (!repos || repos.length === 0) {
        list.innerHTML = '<div style="color:var(--text-secondary)">No repositories tracked. We will auto-detect them.</div>';
        return;
    }
    
    list.innerHTML = repos.map(repo => `
        <div class="list-item">
            <div>
                <h4>${repo.repo_name}</h4>
                <div style="font-size:12px; color:var(--text-secondary); margin-top:4px;">${repo.local_path}</div>
            </div>
            <div>
                <a href="https://github.com/${repo.github_repo}" target="_blank" style="color:var(--accent-blue); text-decoration:none; margin-right:12px;">Open in GitHub</a>
                <button class="btn btn-secondary" onclick="forceSyncRepo('${repo.local_path.replace(/\\/g, '\\\\')}')">Sync Now</button>
            </div>
        </div>
    `).join('');
}

async function forceSyncRepo(path) {
    await fetch('/api/sync', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ local_path: path })
    });
    alert('Sync triggered');
}

function renderDeletions(dels) {
    const list = document.getElementById('deletionsList');
    if (!dels || dels.length === 0) {
        list.innerHTML = '<div style="color:var(--text-secondary)">No pending deletions. You are safe.</div>';
        return;
    }

    list.innerHTML = dels.map(d => `
        <div class="list-item" style="border-color: rgba(239,68,68,0.3)">
            <div>
                <h4 style="color:var(--accent-red)">${d.repo_name}</h4>
                <div style="font-size:12px; color:var(--text-secondary); margin-top:4px;">Detected: ${new Date(d.detected_time).toLocaleString()}</div>
            </div>
            <div style="display:flex; gap:8px;">
                <button class="btn btn-secondary" onclick="cancelDeletion('${d.repo_name}')">Cancel / Ignore</button>
                <button class="btn btn-danger" onclick="confirmDelete('${d.repo_name}')">Delete on GitHub</button>
            </div>
        </div>
    `).join('');
}

async function confirmDelete(name) {
    if(!confirm('This will literally delete the repo on GitHub securely. Are you 100% sure?')) return;
    await fetch('/api/delete-repo', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo_name: name })
    });
    loadData();
}

async function cancelDeletion(name) {
    await fetch('/api/cancel-deletion', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo_name: name })
    });
    loadData();
}

function renderLogs(logs) {
    const container = document.getElementById('logsContainer');
    // Check if user is scrolled to bottom
    const isScrolledToBottom = container.scrollHeight - container.clientHeight <= container.scrollTop + 1;
    
    container.innerHTML = logs.map(l => {
        const time = new Date(l.timestamp).toLocaleTimeString();
        return `<div class="log-line log-${l.level}">[${time}] [${l.level}] ${l.message}</div>`;
    }).reverse().join(''); // Reverse to show latest at bottom
    
    if (isScrolledToBottom) {
        container.scrollTop = container.scrollHeight;
    }
}
