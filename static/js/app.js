// PREMIUM GALLERY APPLICATION
class TroughApp {
    constructor() {
        this.images = [];
        this.page = 1;
        this.loading = false;
        this.hasMore = true;
        this.currentUser = null;
        
        // DOM elements
        this.gallery = document.getElementById('gallery');
        this.lightbox = document.getElementById('lightbox');
        this.uploadZone = document.getElementById('upload-zone');
        this.authModal = document.getElementById('auth-modal');
        this.authBtn = document.getElementById('auth-btn');
        
        // Fixed container above gallery for profile header/upload/bio
        this.profileTop = document.getElementById('profile-top');
        if (!this.profileTop) {
            this.profileTop = document.createElement('div');
            this.profileTop.id = 'profile-top';
            this.profileTop.style.cssText = 'max-width:960px;margin:120px auto 0;padding:0 16px;display:grid;gap:12px';
            // Insert before gallery
            if (this.gallery && this.gallery.parentNode) {
                this.gallery.parentNode.insertBefore(this.profileTop, this.gallery);
            }
        }

        const cachedUser = localStorage.getItem('user');
        if (cachedUser) {
            try { this.currentUser = JSON.parse(cachedUser); } catch {}
            if (this.currentUser?.username) {
                this.authBtn.textContent = `@${this.currentUser.username}`;
            }
        }
        
        this.init();
    }

    async init() {
        await this.checkAuth();
        this.setupEventListeners();

        if (location.pathname.startsWith('/@')) {
            const username = decodeURIComponent(location.pathname.slice(2));
            await this.renderProfilePage(username);
            return;
        }
        if (location.pathname === '/settings') {
            await this.renderSettingsPage();
            return;
        }
        // Not a profile/settings page, clear profileTop
        if (this.profileTop) this.profileTop.innerHTML = '';
        await this.loadImages();
        this.setupInfiniteScroll();
    }

    async checkAuth() {
        const token = localStorage.getItem('token');
        if (!token) {
            this.currentUser = null;
            this.updateAuthButton();
            return;
        }
        try {
            const resp = await fetch('/api/me', { headers: { 'Authorization': `Bearer ${token}` } });
            if (resp.ok) {
                const data = await resp.json();
                this.currentUser = data.user;
                localStorage.setItem('user', JSON.stringify(data.user));
            } else {
                this.signOut();
            }
        } catch {}
        this.updateAuthButton();
    }

    updateAuthButton() {
        if (this.currentUser) {
            this.authBtn.textContent = `@${this.currentUser.username}`;
        } else {
            this.authBtn.textContent = 'ENTER';
        }
    }

    setupEventListeners() {
        // Profile button always goes to profile
        this.authBtn.addEventListener('click', () => {
            if (this.currentUser) {
                window.location.href = `/@${encodeURIComponent(this.currentUser.username)}`;
            } else {
                this.showAuthModal();
            }
        });

        this.setupAuthModal();
        this.setupUpload();
        this.setupLightbox();

        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                this.closeAuthModal();
                this.closeLightbox();
            }
        });
    }

    setupAuthModal() {
        const tabs = document.querySelectorAll('.auth-tab');
        const form = document.getElementById('auth-form');
        const loginForm = document.getElementById('login-form');
        const registerForm = document.getElementById('register-form');
        const submitBtn = document.getElementById('auth-submit');
        const setSubmit = (text, disabled) => { submitBtn.textContent = text; submitBtn.disabled = !!disabled; };

        // Toggle handlers placed below inputs (using data-for attr)
        const bindPasswordToggles = () => {
            document.querySelectorAll('.password-toggle').forEach(btn => {
                btn.onclick = () => {
                    const targetId = btn.getAttribute('data-for');
                    const input = document.getElementById(targetId);
                    if (!input) return;
                    const isPass = input.type === 'password';
                    input.type = isPass ? 'text' : 'password';
                    btn.textContent = isPass ? 'Hide password' : 'Show password';
                };
            });
        };

        const strengthEl = document.getElementById('password-strength');
        const scorePassword = (pwd) => {
            let score = 0;
            if (!pwd) return 0;
            if (pwd.length >= 8) score += 1;
            if (/[A-Z]/.test(pwd)) score += 1;
            if (/[a-z]/.test(pwd)) score += 1;
            if (/[0-9]/.test(pwd)) score += 1;
            if (/[^A-Za-z0-9]/.test(pwd)) score += 1;
            return Math.min(score, 5);
        };
        const renderStrength = (pwd) => {
            const score = scorePassword(pwd);
            const pct = [0, 20, 40, 60, 80, 100][score];
            const color = score >= 4 ? 'var(--color-ok)' : score >= 3 ? 'var(--color-warn)' : 'var(--color-danger)';
            if (strengthEl) {
                strengthEl.style.setProperty('--strength', pct + '%');
                strengthEl.style.setProperty('background', 'var(--border)');
                strengthEl.style.setProperty('boxShadow', 'inset 0 0 0 1px rgba(255,255,255,0.02)');
                strengthEl.style.setProperty('--bar', color);
                // Use after background color
                const after = document.createElement('style');
                after.innerHTML = `#password-strength::after{background:${color}}`;
                document.head.appendChild(after);
            }
            return score;
        };

        // Tab switching
        tabs.forEach(tab => {
            tab.addEventListener('click', () => {
                tabs.forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                
                const tabType = tab.dataset.tab;
                if (tabType === 'login') {
                    loginForm.style.display = 'block';
                    registerForm.style.display = 'none';
                    setSubmit('Sign In', false);
                } else {
                    loginForm.style.display = 'none';
                    registerForm.style.display = 'block';
                    setSubmit('Create Account', false);
                }
                
                // Clear any errors
                this.hideAuthError();
                bindPasswordToggles();
            });
        });

        // Live strength meter
        const registerPassword = document.getElementById('register-password');
        if (registerPassword) {
            registerPassword.addEventListener('input', (e) => renderStrength(e.target.value));
        }

        // Form submission
        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            const isLogin = document.querySelector('.auth-tab.active').dataset.tab === 'login';
            setSubmit(isLogin ? 'Signing In…' : 'Creating…', true);
            
            try {
                if (isLogin) {
                    await this.handleLogin();
                } else {
                    await this.handleRegister();
                }
            } finally {
                setSubmit(isLogin ? 'Sign In' : 'Create Account', false);
            }
        });

        // Bind toggles on init
        bindPasswordToggles();
    }

    async handleLogin() {
        const email = document.getElementById('login-email').value.trim();
        const password = document.getElementById('login-password').value;

        if (!email || !password) {
            this.showAuthError('Please fill in all fields');
            return;
        }

        this.showLoader();
        this.hideAuthError();

        try {
            const response = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, password })
            });

            if (response.status === 401) {
                localStorage.removeItem('token');
                localStorage.removeItem('user');
            }

            const data = await response.json().catch(() => ({}));

            if (response.ok) {
                localStorage.setItem('token', data.token);
                localStorage.setItem('user', JSON.stringify(data.user));
                this.currentUser = data.user;
                this.closeAuthModal();
                this.updateAuthButton();
                this.showNotification('Welcome back!', 'success');
            } else {
                this.showAuthError(data.error || 'Login failed');
            }
        } catch (error) {
            console.error('Login error:', error);
            this.showAuthError('Connection error. Please try again.');
        }

        this.hideLoader();
    }

    async handleRegister() {
        const username = document.getElementById('register-username').value.trim();
        const email = document.getElementById('register-email').value.trim();
        const password = document.getElementById('register-password').value;
        const confirm = document.getElementById('register-password-confirm').value;

        if (!username || !email || !password || !confirm) {
            this.showAuthError('Please fill in all fields');
            return;
        }

        if (password !== confirm) {
            this.showAuthError('Passwords do not match');
            return;
        }

        const score = (pwd => {
            let s = 0; if (pwd.length >= 8) s++; if (/[A-Z]/.test(pwd)) s++; if (/[a-z]/.test(pwd)) s++; if (/[0-9]/.test(pwd)) s++; if (/[^A-Za-z0-9]/.test(pwd)) s++; return Math.min(s,5);
        })(password);
        if (score < 3) {
            this.showAuthError('Password too weak. Add length, numbers, symbols.');
            return;
        }

        this.showLoader();
        this.hideAuthError();

        try {
            const response = await fetch('/api/register', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, email, password })
            });

            if (response.status === 401) {
                localStorage.removeItem('token');
                localStorage.removeItem('user');
            }

            const data = await response.json().catch(() => ({}));

            if (response.ok) {
                localStorage.setItem('token', data.token);
                localStorage.setItem('user', JSON.stringify(data.user));
                this.currentUser = data.user;
                this.closeAuthModal();
                this.updateAuthButton();
                this.showNotification(`Welcome to TROUGH, ${data.user.username}!`, 'success');
            } else {
                this.showAuthError(data.error || 'Registration failed');
            }
        } catch (error) {
            console.error('Registration error:', error);
            this.showAuthError('Connection error. Please try again.');
        }

        this.hideLoader();
    }

    showAuthModal() {
        this.authModal.classList.add('active');
        document.body.style.overflow = 'hidden';
        
        // Clear form and errors
        document.getElementById('auth-form').reset();
        this.hideAuthError();
        
        // Focus first input
        setTimeout(() => {
            const activeTab = document.querySelector('.auth-tab.active')?.dataset.tab || 'login';
            const firstInput = activeTab === 'login' ? document.getElementById('login-email') : document.getElementById('register-username');
            if (firstInput) firstInput.focus();
        }, 100);
    }

    closeAuthModal() {
        this.authModal.classList.remove('active');
        document.body.style.overflow = '';
    }

    showAuthError(message) {
        const errorDiv = document.getElementById('auth-error');
        errorDiv.textContent = message;
        errorDiv.style.display = 'block';
    }

    hideAuthError() {
        const errorDiv = document.getElementById('auth-error');
        errorDiv.style.display = 'none';
    }

    // Markdown sanitizer + minimal renderer (bold/italic/links)
    sanitizeAndRenderMarkdown(md) {
        const maxLen = 2000;
        let text = (md || '').slice(0, maxLen);
        // Escape HTML
        text = text.replace(/[&<>"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;','\'':'&#39;'}[ch]));
        // Links [text](url)
        text = text.replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, (m, label, url) => {
            try {
                const u = new URL(url);
                const safe = ['http:', 'https:'].includes(u.protocol);
                return safe ? `<a href="${u.href}" target="_blank" rel="noopener noreferrer">${label}</a>` : label;
            } catch { return label; }
        });
        // Bold **text** and italic *text*
        text = text.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
        text = text.replace(/\*([^*]+)\*/g, '<em>$1</em>');
        // Line breaks
        text = text.replace(/\n/g, '<br>');
        return text;
    }

    async renderProfilePage(username) {
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.innerHTML = '';

        let user = null; let imgs = { images: [] };
        try {
            const [u, i] = await Promise.all([
                fetch(`/api/users/${encodeURIComponent(username)}`),
                fetch(`/api/users/${encodeURIComponent(username)}/images?page=1`)
            ]);
            if (!u.ok) throw new Error('User not found');
            user = await u.json();
            imgs = i.ok ? await i.json() : { images: [] };
        } catch (e) {
            this.showNotification('Profile not found', 'error');
            return;
        }
        const isOwner = this.currentUser && (this.currentUser.username === user.username);

        // Header (no backdrop)
        const header = document.createElement('section');
        header.className = 'mono-col';
        header.style.cssText = 'padding:16px 0;color:var(--text-primary);display:flex;align-items:center;justify-content:space-between;gap:12px;margin:0 auto';
        const avatar = `<div class="avatar-preview" style="background-image:url('${user.avatar_url || ''}');"></div>`;
        header.innerHTML = `
          <div style="display:flex;gap:12px;align-items:center">
            ${avatar}
            <div style="font-weight:700;font-size:1.1rem">@${user.username}</div>
          </div>
          ${isOwner ? '<button id="profile-settings" class="link-btn">Settings</button>' : ''}
        `;
        this.profileTop.appendChild(header);
        if (isOwner) {
            document.getElementById('profile-settings').onclick = () => { history.pushState({}, '', '/settings'); this.renderSettingsPage(); };
        }

        // Upload panel (owner only) unchanged styling minimal
        if (isOwner) {
            const uploadPanel = document.createElement('section');
            uploadPanel.className = 'mono-col';
            uploadPanel.style.cssText = 'padding:0 0 8px;color:var(--text-primary);margin:0 auto';
            uploadPanel.innerHTML = `
              <div id="profile-drop" style="display:flex;gap:12px;align-items:center;justify-content:center;padding:12px;border:1px dashed var(--border-strong);border-radius:10px;background:var(--surface)">
                <button id="profile-file" class="nav-btn" style="padding:8px 14px;font-size:0.9rem;">Choose files</button>
                <span style="color:var(--text-secondary)">or drag & drop here</span>
              </div>
            `;
            this.profileTop.appendChild(uploadPanel);
            // handlers as before
            const drop = uploadPanel.querySelector('#profile-drop');
            const fileBtn = uploadPanel.querySelector('#profile-file');
            const pick = document.createElement('input'); pick.type = 'file'; pick.accept = 'image/*'; pick.multiple = true; pick.style.display = 'none'; uploadPanel.appendChild(pick);
            const handleFiles = async (files) => {
                for (const f of files) { await this.uploadImage(f, {}); }
                const resp = await fetch(`/api/users/${encodeURIComponent(username)}/images?page=1`);
                const data = await resp.json().catch(()=>({images:[]}));
                this.gallery.innerHTML = ''; (data.images || []).forEach((img) => this.createImageCard(img));
            };
            fileBtn.addEventListener('click', () => pick.click());
            pick.addEventListener('change', (e) => handleFiles(Array.from(e.target.files || [])));
            ['dragenter','dragover'].forEach(ev => drop.addEventListener(ev, e => { e.preventDefault(); drop.style.borderColor = 'var(--text-primary)'; }));
            ['dragleave','drop'].forEach(ev => drop.addEventListener(ev, e => { e.preventDefault(); drop.style.borderColor = 'var(--border)'; }));
            drop.addEventListener('drop', (e) => handleFiles(Array.from(e.dataTransfer.files || [])));
        }

        // Inline bio with edit for owner
        const bioText = (typeof user.bio === 'string') ? user.bio.trim() : '';
        const bio = document.createElement('section');
        bio.className = 'mono-col';
        bio.style.cssText = 'padding:8px 0 16px;margin:0 auto;color:var(--text-primary)';
        const editBtn = isOwner ? '<button id="bio-edit" class="link-btn">Edit</button>' : '';
        bio.innerHTML = `
          <div style="display:flex;align-items:flex-start;gap:8px;justify-content:space-between">
            <div id="bio-view" class="user-bio" style="flex:1">${bioText ? this.sanitizeAndRenderMarkdown(bioText) : ''}</div>
            ${editBtn}
          </div>
        `;
        this.profileTop.appendChild(bio);
        if (isOwner) {
            const btn = bio.querySelector('#bio-edit');
            btn.onclick = () => {
                const area = document.createElement('div');
                area.className = 'mono-col';
                area.style.cssText = 'margin:0 auto';
                area.innerHTML = `
                  <textarea id="bio-input" rows="8" maxlength="500" class="settings-input" style="width:100%">${(bioText || '')}</textarea>
                  <div style="display:flex;justify-content:space-between;align-items:center;margin-top:6px">
                    <small id="bio-count" style="color:var(--text-secondary)"></small>
                    <div style="display:flex;gap:8px">
                      <button id="bio-cancel" class="nav-btn">Cancel</button>
                      <button id="bio-save" class="nav-btn">Save</button>
                    </div>
                  </div>`;
                bio.replaceWith(area);
                const input = area.querySelector('#bio-input');
                const count = area.querySelector('#bio-count');
                const updateCount = () => { count.textContent = `${input.value.length}/500`; };
                input.addEventListener('input', updateCount); updateCount();
                area.querySelector('#bio-cancel').onclick = () => { this.renderProfilePage(username); };
                area.querySelector('#bio-save').onclick = async () => {
                    const resp = await fetch('/api/me/profile', { method:'PATCH', headers: { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ bio: input.value.slice(0,500) }) });
                    if (resp.ok) { this.showNotification('Bio updated'); this.renderProfilePage(username); }
                    else { const err = await resp.json().catch(()=>({})); this.showNotification(err.error||'Save failed','error'); }
                };
            };
        }

        (imgs.images || []).forEach((img) => this.createImageCard(img));
    }

    // Gallery/loading functions
    async loadImages() {
        if (this.loading || !this.hasMore) return;
        this.loading = true;
        this.showLoader();
        try {
            const resp = await fetch(`/api/feed?page=${this.page}`);
            if (resp.ok) {
                const data = await resp.json();
                if (data.images && data.images.length > 0) {
                    this.renderImages(data.images);
                    this.page++;
                } else {
                    this.hasMore = false;
                }
            } else {
                if (this.page === 1) this.renderDemoImages();
                this.hasMore = false;
            }
        } catch (e) {
            if (this.page === 1) this.renderDemoImages();
            this.hasMore = false;
        } finally {
            this.loading = false;
            this.hideLoader();
        }
    }

    renderDemoImages() {
        const demoImages = [
            { id: '1', title: 'Neural Genesis', author: 'AI_PROPHET', color: '#2563eb', width: 400, height: 600 },
            { id: '2', title: 'Digital Dreams', author: 'CODE_MYSTIC', color: '#7c3aed', width: 400, height: 500 },
            { id: '3', title: 'Synthetic Vision', author: 'PIXEL_SAGE', color: '#059669', width: 400, height: 700 },
            { id: '4', title: 'Quantum Echo', author: 'DATA_SHAMAN', color: '#dc2626', width: 400, height: 450 },
            { id: '5', title: 'Electric Soul', author: 'BIT_ORACLE', color: '#ea580c', width: 400, height: 650 },
            { id: '6', title: 'Cyber Meditation', author: 'TECHNO_ZEN', color: '#9333ea', width: 400, height: 550 },
            { id: '7', title: 'Virtual Essence', author: 'GHOST_CODER', color: '#0891b2', width: 400, height: 480 },
            { id: '8', title: 'Binary Poetry', author: 'NULL_ARTIST', color: '#4f46e5', width: 400, height: 620 },
            { id: '9', title: 'Algorithmic Beauty', author: 'MATH_WIZARD', color: '#be185d', width: 400, height: 580 },
            { id: '10', title: 'Machine Dreams', author: 'ROBO_ARTIST', color: '#0d9488', width: 400, height: 520 }
        ];
        demoImages.forEach((image, index) => {
            setTimeout(() => this.createImageCard(image), index * 120);
        });
    }

    renderImages(images) {
        images.forEach((img, index) => setTimeout(() => this.createImageCard(img), index * 80));
    }

    createImageCard(image) {
        const card = document.createElement('div');
        card.className = 'image-card';
        card.style.animationDelay = `${Math.random() * 0.5}s`;

        const isDemo = !image.filename;
        const onProfile = location.pathname.startsWith('/@');
        const isOwner = !!this.currentUser && (this.currentUser.username === image.username);
        const isAdmin = !!this.currentUser && !!this.currentUser.is_admin;
        const canEdit = (onProfile && isOwner) || isAdmin;

        if (isDemo) {
            card.innerHTML = `
                <div style="aspect-ratio:${image.width} / ${image.height};background:linear-gradient(135deg, ${image.color}dd, ${image.color}88);display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden;">
                    <div style="position:absolute;inset:0;background:url('data:image/svg+xml,<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"20\" height=\"20\" viewBox=\"0 0 20 20\" fill=\"none\"><circle cx=\"10\" cy=\"10\" r=\"1\" fill=\"white\" opacity=\"0.1\"/></svg>') repeat;opacity:0.3;"></div>
                    <div style="color:white;font-size:1.25rem;font-weight:600;text-align:center;opacity:0.85;z-index:1;padding:2rem;text-shadow:0 2px 4px rgba(0,0,0,0.3)">${image.title}</div>
                </div>
                <div class="image-meta">
                    <div class="image-title">${image.title}</div>
                    <div class="image-author">@${image.author}</div>
                </div>`;
        } else {
            const img = document.createElement('img');
            img.src = `/uploads/${image.filename}`;
            img.alt = image.original_name || image.title || '';
            img.loading = 'lazy';
            card.appendChild(img);

            const meta = document.createElement('div');
            meta.className = 'image-meta';
            const username = image.username || image.author || 'Unknown';
            const captionHtml = image.caption ? `<div class="image-caption" style="margin-top:4px;color:var(--text-secondary);font-size:0.8rem">${(image.caption || '').slice(0, 2000)}</div>` : '';
            const actions = canEdit ? `
                <div class="image-actions" style="display:flex;gap:8px;align-items:center;flex-shrink:0">
                  <button title="Edit" class="like-btn" data-act="edit" data-id="${image.id}" style="width:28px;height:28px;color:var(--text-secondary)">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 1 1 3 3L7 19l-4 1 1-4Z"/></svg>
                  </button>
                  <button title="Delete" class="like-btn" data-act="delete" data-id="${image.id}" style="width:28px;height:28px;color:#ff6666">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </div>` : '';
            meta.innerHTML = `
                <div style="display:flex;align-items:center;justify-content:space-between;gap:12px">
                  <div style="min-width:0">
                    <div class="image-title" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${(image.title || image.original_name || 'Untitled').trim()}</div>
                    <div class="image-author"><a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${username}</a></div>
                  </div>
                  ${actions}
                </div>
                ${captionHtml}`;
            meta.addEventListener('click', async (e) => {
                const btn = e.target.closest('button');
                if (!btn) return;
                const act = btn.dataset.act;
                const id = btn.dataset.id;
                e.stopPropagation();
                if (act === 'delete') {
                    const ok = confirm('Delete image?');
                    if (ok) {
                        const resp = await fetch(`/api/images/${id}`, { method: 'DELETE', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }});
                        if (resp.status === 204) { card.remove(); this.showNotification('Image deleted'); } else { this.showNotification('Delete failed', 'error'); }
                    }
                } else if (act === 'edit') {
                    this.openEditModal(image, card);
                }
            });
            card.appendChild(meta);
        }

        card.addEventListener('click', () => this.openLightbox(image));
        this.gallery.appendChild(card);
    }

    async openEditModal(image, cardNode) {
        let filename = image.filename;
        if (!filename && image.id) {
            try { const r = await fetch(`/api/images/${image.id}`); if (r.ok) { const d = await r.json(); filename = d.filename; } } catch {}
        }
        const overlay = document.createElement('div');
        overlay.style.cssText = 'position:fixed;inset:0;z-index:2700;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
        const panel = document.createElement('div');
        panel.style.cssText = 'max-width:980px;width:100%;max-height:90vh;overflow:auto;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
        panel.innerHTML = `
            ${filename ? `<div style="display:flex;justify-content:center;"><img src="/uploads/${filename}" alt="" style="max-height:60vh;width:auto;border-radius:10px;border:1px solid var(--border);margin-bottom:12px"/></div>` : ''}
            <div style="position:sticky;bottom:0;background:var(--surface-elevated);border-top:1px solid var(--border);padding-top:12px;display:grid;gap:12px">
              <input id="e-title" placeholder="Title" value="${(image.title || image.original_name || '').replaceAll('"','&quot;')}" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary)"/>
              <textarea id="e-caption" placeholder="Caption" rows="3" maxlength="2000" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary)">${(image.caption||'').replaceAll('<','&lt;')}</textarea>
              <label style="display:flex;gap:8px;align-items:center;color:var(--text-secondary)"><input type="checkbox" id="e-nsfw" ${image.is_nsfw ? 'checked' : ''}/> NSFW</label>
              <div style="display:flex;gap:8px;justify-content:flex-end">
                <button id="e-cancel" class="nav-btn">Cancel</button>
                <button id="e-save" class="nav-btn">Save</button>
              </div>
            </div>`;
        overlay.appendChild(panel);
        overlay.addEventListener('click', (e) => { if (e.target === overlay) overlay.remove(); });
        document.body.appendChild(overlay);
        panel.querySelector('#e-cancel').onclick = () => overlay.remove();
        panel.querySelector('#e-save').onclick = async () => {
            const body = { title: panel.querySelector('#e-title').value, caption: panel.querySelector('#e-caption').value, is_nsfw: panel.querySelector('#e-nsfw').checked };
            const resp = await fetch(`/api/images/${image.id}`, { method:'PATCH', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
            if (resp.ok) { overlay.remove(); this.showNotification('Saved'); location.reload(); } else { this.showNotification('Save failed', 'error'); }
        };
    }

    openLightbox(image) {
        const lightboxImg = document.getElementById('lightbox-img');
        const lightboxTitle = document.getElementById('lightbox-title');
        const lightboxAuthor = document.getElementById('lightbox-author');
        const lightboxLike = document.getElementById('lightbox-like');
        if (!lightboxImg) return;

        if (image.filename) {
            lightboxImg.src = `/uploads/${image.filename}`;
            lightboxImg.alt = image.original_name || image.title || '';
        }
        const username = image.username || image.author || 'Unknown';
        lightboxTitle.textContent = image.title || image.original_name || 'Untitled';
        lightboxAuthor.innerHTML = `<a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${username}</a>`;
        lightboxLike.classList.remove('liked');
        lightboxLike.onclick = () => this.toggleLike(image.id);
        this.lightbox.classList.add('active');
        document.body.style.overflow = 'hidden';
    }

    closeLightbox() {
        this.lightbox.classList.remove('active');
        document.body.style.overflow = '';
    }

    setupLightbox() {
        // Backdrop close handled in HTML; ESC handled globally
    }

    async toggleLike(imageId) {
        if (!this.currentUser) { this.showAuthModal(); return; }
        const likeBtn = document.getElementById('lightbox-like');
        const wasLiked = likeBtn.classList.contains('liked');
        likeBtn.classList.toggle('liked');
        try {
            const response = await fetch(`/api/images/${imageId}/like`, { method: 'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type': 'application/json' }});
            if (!response.ok) {
                likeBtn.classList.toggle('liked');
                if (response.status === 401) { localStorage.removeItem('token'); localStorage.removeItem('user'); this.currentUser = null; this.checkAuth(); this.showAuthModal(); }
                else { this.showNotification('Like failed', 'error'); }
            }
        } catch { likeBtn.classList.toggle('liked'); }
    }

    setupUpload() {
        let dragCounter = 0;
        const handleDragEnter = (e) => { e.preventDefault(); dragCounter++; if (dragCounter === 1) this.uploadZone.classList.add('active'); };
        const handleDragLeave = (e) => { e.preventDefault(); dragCounter--; if (dragCounter === 0) this.uploadZone.classList.remove('active'); };
        const handleDragOver = (e) => { e.preventDefault(); };
        const handleDrop = async (e) => {
            e.preventDefault(); dragCounter = 0; this.uploadZone.classList.remove('active');
            if (!this.currentUser) { this.showAuthModal(); return; }
            const files = Array.from(e.dataTransfer.files).filter(f => f.type.startsWith('image/'));
            if (files.length === 0) { this.showNotification('Please drop image files only', 'error'); return; }
            for (const file of files) {
                const uploaded = await this.uploadImage(file, {});
                if (uploaded) {
                    this.openEditModal({ id: uploaded.id, original_name: uploaded.original_name, caption: uploaded.caption || '', is_nsfw: false, filename: uploaded.filename }, null);
                }
            }
        };
        document.addEventListener('dragenter', handleDragEnter);
        document.addEventListener('dragleave', handleDragLeave);
        document.addEventListener('dragover', handleDragOver);
        document.addEventListener('drop', handleDrop);
    }

    async promptImageMeta() {
        return new Promise((resolve) => {
            const overlay = document.createElement('div');
            overlay.style.cssText = 'position:fixed;inset:0;z-index:2600;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
            const panel = document.createElement('div');
            panel.style.cssText = 'max-width:520px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
            panel.innerHTML = `
                <div style="font-weight:600;margin-bottom:8px">Image details</div>
                <input id="m-title" placeholder="Title (optional)" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary);margin-bottom:8px"/>
                <textarea id="m-caption" placeholder="Caption (optional)" rows="2" maxlength="2000" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary);margin-bottom:8px"></textarea>
                <label style="display:flex;gap:8px;align-items:center;margin-bottom:12px;color:var(--text-secondary)"><input type="checkbox" id="m-nsfw"/> NSFW</label>
                <div style="display:flex;gap:8px;justify-content:flex-end">
                  <button id="m-cancel" class="nav-btn">Cancel</button>
                  <button id="m-ok" class="nav-btn">Upload</button>
                </div>`;
            overlay.appendChild(panel);
            document.body.appendChild(overlay);
            panel.querySelector('#m-cancel').onclick = () => { overlay.remove(); resolve(null); };
            panel.querySelector('#m-ok').onclick = () => {
                const title = panel.querySelector('#m-title').value;
                const caption = panel.querySelector('#m-caption').value;
                const nsfw = panel.querySelector('#m-nsfw').checked;
                overlay.remove();
                resolve({ title, caption, nsfw });
            };
        });
    }

    async renderSettingsPage() {
        if (!this.currentUser) { this.showAuthModal(); return; }
        let email = '';
        try { const resp = await fetch('/api/me/account', { headers: { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}` } }); if (resp.ok) { const acc = await resp.json(); email = acc.email || ''; } } catch {}

        this.gallery.innerHTML = '';
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.classList.add('settings-mode');
        const wrap = document.createElement('div');
        wrap.className = 'settings-wrap';
        const avatarURL = (this.currentUser && this.currentUser.avatar_url) ? this.currentUser.avatar_url : '';
        wrap.innerHTML = `
          <section class="settings-group">
            <div class="settings-label">Username</div>
            <input type="text" id="settings-username" value="${this.currentUser.username}" minlength="3" maxlength="30" class="settings-input"/>
            <div class="settings-actions"><button id="btn-username" class="nav-btn">Change Username</button></div>
            <div class="form-error" id="err-username" style="color:#ff5c5c;font-size:0.8rem"></div>
          </section>
          <section class="settings-group">
            <div class="settings-label">Avatar</div>
            <div class="avatar-row">
              <div class="avatar-preview" id="avatar-preview" style="background-image:url('${avatarURL}')"></div>
              <input type="file" id="avatar-file" accept="image/*"/>
              <button id="avatar-upload" class="nav-btn">Upload</button>
            </div>
          </section>
          <section class="settings-group">
            <div class="settings-label">Email</div>
            <input type="email" id="settings-email" value="${email}" class="settings-input"/>
            <div class="settings-actions"><button id="btn-email" class="nav-btn">Save Email</button></div>
            <div class="form-error" id="err-email" style="color:#ff5c5c;font-size:0.8rem"></div>
          </section>
          <section class="settings-group">
            <div class="settings-label">Password</div>
            <input type="password" id="current-password" placeholder="Current password" class="settings-input"/>
            <input type="password" id="new-password" placeholder="New password" minlength="6" class="settings-input"/>
            <input type="password" id="new-password-confirm" placeholder="Confirm new password" minlength="6" class="settings-input"/>
            <div id="pw-strength" style="height:6px;width:100%;background:var(--border);border-radius:999px;overflow:hidden"><div id="pw-bar" style="height:6px;width:0;background:var(--color-danger)"></div></div>
            <div class="settings-actions"><button id="btn-password" class="nav-btn">Change Password</button></div>
            <div class="form-error" id="err-password" style="color:#ff5c5c;font-size:0.8rem"></div>
          </section>
          <section class="settings-group">
            <div class="settings-label" style="color:#ff5c5c">Delete</div>
            <div class="settings-actions" style="gap:8px;align-items:center">
              <input type="text" id="delete-confirm" placeholder="Type 'DELETE' to confirm" class="settings-input" style="flex:1"/>
              <button id="btn-delete" class="nav-btn" style="background: var(--color-danger); border: 1px solid var(--color-danger); color:#fff">Delete account</button>
            </div>
            <div class="form-error" id="err-delete" style="color:#ff5c5c;font-size:0.8rem"></div>
          </section>`;
        this.gallery.appendChild(wrap);

        // Password strength + confirm
        const pw = document.getElementById('new-password');
        const pwc = document.getElementById('new-password-confirm');
        const bar = document.getElementById('pw-bar');
        const scorePassword = (pwd) => {
          let s = 0; if (!pwd) return 0; if (pwd.length >= 8) s++; if (/[A-Z]/.test(pwd)) s++; if (/[a-z]/.test(pwd)) s++; if (/[0-9]/.test(pwd)) s++; if (/[^A-Za-z0-9]/.test(pwd)) s++; return Math.min(s,5);
        };
        const renderBar = () => { const score = scorePassword(pw.value); const pct = [0,20,40,60,80,100][score]; const ok = score>=4; bar.style.width = pct+'%'; bar.style.background = ok ? 'var(--color-ok)' : score>=3 ? 'var(--color-warn)' : 'var(--color-danger)'; };
        pw.addEventListener('input', renderBar); renderBar();

        // Back navigation: handle popstate to rerender profile
        window.onpopstate = () => {
            if (location.pathname.startsWith('/@')) {
                this.gallery.classList.remove('settings-mode');
                const u = decodeURIComponent(location.pathname.slice(2));
                this.renderProfilePage(u);
            } else {
                this.gallery.classList.remove('settings-mode');
                this.gallery.innerHTML = ''; if (this.profileTop) this.profileTop.innerHTML=''; this.page=1; this.hasMore=true; this.loadImages();
            }
        };

        // Handlers
        const authHeader = { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}`, 'Content-Type': 'application/json' };
        document.getElementById('btn-username').onclick = async () => {
            const username = document.getElementById('settings-username').value.trim();
            try { const resp = await fetch('/api/me/profile', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ username }) }); if (!resp.ok) throw await resp.json(); const userResp = await resp.json(); this.currentUser=userResp; localStorage.setItem('user', JSON.stringify(userResp)); this.updateAuthButton(); this.showNotification('Username changed'); } catch (e) { document.getElementById('err-username').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-email').onclick = async () => {
            const v = document.getElementById('settings-email').value.trim();
            try { const resp = await fetch('/api/me/email', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ email: v }) }); if (!resp.ok) throw await resp.json(); this.showNotification('Email updated'); } catch (e) { document.getElementById('err-email').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-password').onclick = async () => {
            const current = document.getElementById('current-password').value; const next = pw.value; const confirm = pwc.value;
            if (next !== confirm) { document.getElementById('err-password').textContent = 'Passwords do not match'; return; }
            try { const resp = await fetch('/api/me/password', { method:'PATCH', headers: authHeader, body: JSON.stringify({ current_password: current, new_password: next }) }); if (resp.status !== 204) throw await resp.json(); document.getElementById('current-password').value=''; pw.value=''; pwc.value=''; renderBar(); this.showNotification('Password changed'); } catch (e) { document.getElementById('err-password').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-delete').onclick = async () => {
            const conf = document.getElementById('delete-confirm').value.trim(); if (conf !== 'DELETE') { document.getElementById('err-delete').textContent='Type DELETE to confirm'; return; }
            try { const resp = await fetch('/api/me', { method:'DELETE', headers: authHeader, body: JSON.stringify({ confirm:'DELETE' }) }); if (resp.status !== 204) throw await resp.json(); this.signOut(); window.location.href='/'; } catch (e) { document.getElementById('err-delete').textContent = e.error || 'Failed'; }
        };

        // Avatar upload
        document.getElementById('avatar-upload').onclick = async () => {
            const fileInput = document.getElementById('avatar-file'); const file = fileInput.files && fileInput.files[0]; if (!file) { this.showNotification('Choose a file first', 'error'); return; }
            const fd = new FormData(); fd.append('avatar', file);
            try { const resp = await fetch('/api/me/avatar', { method:'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}` }, body: fd }); if (!resp.ok) throw await resp.json(); const data = await resp.json(); document.getElementById('avatar-preview').style.backgroundImage = `url('${data.avatar_url}')`; this.currentUser.avatar_url = data.avatar_url; localStorage.setItem('user', JSON.stringify(this.currentUser)); this.showNotification('Avatar updated'); } catch (e) { this.showNotification(e.error || 'Upload failed', 'error'); }
        };
    }

    async uploadImage(file, options = {}) {
        if (file.size > 10 * 1024 * 1024) {
            this.showNotification('Image too large (max 10MB)', 'error');
            return null;
        }

        const formData = new FormData();
        formData.append('image', file);
        if (options.title) formData.append('title', options.title);
        if (typeof options.nsfw === 'boolean') formData.append('is_nsfw', String(options.nsfw));
        if (options.caption) formData.append('caption', options.caption);
        
        this.showLoader();
        
        try {
            const response = await fetch('/api/upload', {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` },
                body: formData
            });
            
            if (response.ok) {
                const image = await response.json();
                this.showNotification('Image uploaded');
                return image;
            } else if (response.status === 401) {
                localStorage.removeItem('token');
                localStorage.removeItem('user');
                this.currentUser = null;
                this.checkAuth();
                this.showAuthModal();
            } else {
                const error = await response.json().catch(() => ({}));
                this.showNotification(`Upload failed: ${error.error || 'Unknown error'}`, 'error');
            }
        } catch (error) {
            this.showNotification('Upload failed', 'error');
        } finally {
            this.hideLoader();
        }
        return null;
    }

    setupInfiniteScroll() {
        let ticking = false;
        
        const handleScroll = () => {
            if (!ticking) {
                requestAnimationFrame(() => {
                    const { scrollTop, scrollHeight, clientHeight } = document.documentElement;
                    
                    if (scrollTop + clientHeight >= scrollHeight - 1000) {
                        this.loadImages();
                    }
                    
                    ticking = false;
                });
                
                ticking = true;
            }
        };
        
        window.addEventListener('scroll', handleScroll, { passive: true });
    }

    showLoader() {
        if (!document.querySelector('.loader')) {
            const loader = document.createElement('div');
            loader.className = 'loader';
            // center the loader
            loader.style.top = '50%';
            loader.style.left = '50%';
            loader.style.bottom = '';
            loader.style.transform = 'translate(-50%, -50%)';
            document.body.appendChild(loader);
        }
    }

    hideLoader() {
        const loader = document.querySelector('.loader');
        if (loader) loader.remove();
    }

    showNotification(message, type = 'info') {
        const notification = document.createElement('div');
        const bgColors = {
            success: '#10b981',
            error: '#ef4444',
            info: '#111',
        };
        notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            background: ${bgColors[type] || bgColors.info};
            color: white;
            padding: 12px 20px;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 500;
            box-shadow: 0 10px 25px rgba(0, 0, 0, 0.2);
            z-index: 10000;
            transform: translateX(100%);
            transition: transform 0.3s cubic-bezier(0.68, -0.55, 0.265, 1.55);
            max-width: 320px;
            word-wrap: break-word;
            border: 1px solid rgba(255,255,255,0.08);
        `;
        notification.textContent = message;
        document.body.appendChild(notification);
        setTimeout(() => { notification.style.transform = 'translateX(0)'; }, 10);
        setTimeout(() => {
            notification.style.transform = 'translateX(100%)';
            setTimeout(() => notification.remove(), 300);
        }, 3000);
    }
}

document.addEventListener('DOMContentLoaded', () => { window.app = new TroughApp(); });