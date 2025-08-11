// PREMIUM GALLERY APPLICATION
class TroughApp {
    constructor() {
        this.images = [];
        this.page = 1;
        this.loading = false;
        this.hasMore = true;
        this.currentUser = null;
        // Mobile magnetic scroll state
        this.magneticEnabled = false;
        this.magneticListenersAttached = false;
        this.magnetOffset = 10; // px distance from viewport top (updated dynamically)
        this.magnetSkipNextSnap = false;
        this.magnetMaxVelocity = 0;
        this.magnetVelocityThreshold = 1.2; // px/ms ~1200px/s counts as a fast flick
        this._magnetSnapTimer = null;
        this._lastTouchY = 0;
        this._lastTouchTime = 0;
        this._activeScrollAnim = null;
        this._lastScrollY = typeof window !== 'undefined' ? window.scrollY : 0;
        this._magnetDirection = 1; // 1 = down, -1 = up
        this._epsilonBackward = 24; // px allowed to snap slightly backwards
        this._topGuardPx = 16; // legacy; no hard first-image forcing now
        this._lastSnapY = -1;
        this._lastSnapEl = null;
        
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
                this.authBtn.style.fontFamily = 'var(--font-mono)';
            }
        }
        
        this.init();
    }

    // Helper function to get the correct image URL (handles both local filenames and remote URLs)
    getImageURL(filename) {
        if (!filename) return '';
        
        // Check for full URLs with protocol
        if (filename.startsWith('http://') || filename.startsWith('https://')) {
            return filename;
        }
        
        // Check for domain-based URLs without protocol (like z.disinfo.zone/file.jpg)
        if (filename.includes('.') && filename.includes('/') && !filename.startsWith('/')) {
            return 'https://' + filename;
        }
        
        // Local filename
        return `/uploads/${filename}`;
    }

    async init() {
        await this.checkAuth();
        await this.applyPublicSiteSettings();
        this.setupEventListeners();

        if (location.pathname === '/reset') { await this.renderResetPage(); return; }
        if (location.pathname === '/verify') { await this.renderVerifyPage(); return; }
        if (location.pathname.startsWith('/@')) {
            const username = decodeURIComponent(location.pathname.slice(2));
            await this.renderProfilePage(username);
            return;
        }
        if (location.pathname === '/settings') {
            await this.renderSettingsPage();
            return;
        }
        if (location.pathname === '/admin') {
            await this.renderAdminPage();
            return;
        }
        if (location.pathname.startsWith('/i/')) {
            const id = location.pathname.split('/')[2];
            await this.renderImagePage(id);
            return;
        }
        // Not a profile/settings page, clear profileTop
        if (this.profileTop) this.profileTop.innerHTML = '';
        await this.loadImages();
        this.setupInfiniteScroll();
        // Ensure logo data-text mirrors current text for blend-mode rendering
        const logo = document.querySelector('.logo');
        if (logo && !logo.getAttribute('data-text')) {
            logo.setAttribute('data-text', logo.textContent || '');
        }
        // Initialize new drift-based MagneticScroll
        this.magneticScroll = new MagneticScroll({
            minCardIndex: 2,
            attractionStrength: 0.012,
            damping: 0.94,
            maxDriftSpeed: 0.6,
            settleDelay: 200,
            effectiveRange: 250,
        });
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) {
            this.magneticScroll.updateEnabledState();
        }
    }

    async applyPublicSiteSettings() {
        try {
            const r = await fetch('/api/site');
            if (!r.ok) return;
            const s = await r.json();
            window.__SITE_EMAIL_ENABLED__ = !!s.email_enabled;
            if (s.from_email) window.__SITE_FROM_EMAIL__ = s.from_email;
            if (s.site_name) {
                const logo = document.querySelector('.logo');
                if (logo) { logo.textContent = s.site_name; logo.setAttribute('data-text', s.site_name); }
                document.title = s.seo_title || `${s.site_name} · AI IMAGERY`;
            }
            if (s.favicon_path) {
                let link = document.querySelector('link[rel="icon"]') || document.createElement('link');
                link.rel = 'icon'; link.href = s.favicon_path + '?v=' + Date.now();
                document.head.appendChild(link);
            }
            const setMeta = (name, content) => {
                if (!content) return;
                let m = document.querySelector(`meta[name="${name}"]`);
                if (!m) { m = document.createElement('meta'); m.setAttribute('name', name); document.head.appendChild(m); }
                m.setAttribute('content', content);
            };
            setMeta('description', s.seo_description || '');
        } catch {}
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
            this.authBtn.style.fontFamily = 'var(--font-mono)';
        } else {
            this.authBtn.textContent = 'ENTER';
            this.authBtn.style.fontFamily = '';
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

    // Sign out clears auth and updates UI
    signOut() {
        localStorage.removeItem('token');
        localStorage.removeItem('user');
        this.currentUser = null;
        this.updateAuthButton();
    }

    setupAuthModal() {
        const tabs = document.querySelectorAll('.auth-tab');
        const form = document.getElementById('auth-form');
        const loginForm = document.getElementById('login-form');
        const registerForm = document.getElementById('register-form');
        const submitBtn = document.getElementById('auth-submit');
        const setSubmit = (text, disabled) => { submitBtn.textContent = text; submitBtn.disabled = !!disabled; };

        // Replace password toggles with inline eye icons
        const ensureEyeToggle = (inputId) => {
            const input = document.getElementById(inputId);
            if (!input) return;
            if (input.parentElement?.classList?.contains('pw-wrap')) return;
            const wrap = document.createElement('div');
            wrap.className = 'pw-wrap';
            wrap.style.position = 'relative';
            input.parentNode.insertBefore(wrap, input);
            wrap.appendChild(input);
            input.style.paddingRight = '40px';
            const eye = document.createElement('button');
            eye.type = 'button'; eye.className = 'pw-eye';
            eye.setAttribute('aria-label', 'Toggle password visibility');
            eye.textContent = '👁';
            eye.style.cssText = 'position:absolute;right:10px;top:50%;transform:translateY(-50%);background:none;border:none;color:var(--text-tertiary);opacity:.8;';
            eye.onclick = () => { const isPass = input.type === 'password'; input.type = isPass ? 'text' : 'password'; eye.textContent = isPass ? '🙈' : '👁'; };
            wrap.appendChild(eye);
        };

        // Build eyes for all password fields and remove old text toggles
        ['login-password','register-password','register-password-confirm'].forEach(id => ensureEyeToggle(id));
        document.querySelectorAll('.password-toggle').forEach(el => el.remove());

        const strengthEl = document.getElementById('password-strength');
        const scorePassword = (pwd) => { let score = 0; if (!pwd) return 0; if (pwd.length >= 8) score += 1; if (/[A-Z]/.test(pwd)) score += 1; if (/[a-z]/.test(pwd)) score += 1; if (/[0-9]/.test(pwd)) score += 1; if (/[^A-Za-z0-9]/.test(pwd)) score += 1; return Math.min(score, 5); };
        const renderStrength = (pwd) => { const score = scorePassword(pwd); const pct = [0,20,40,60,80,100][score]; const color = score >= 4 ? 'var(--color-ok)' : score >= 3 ? 'var(--color-warn)' : 'var(--color-danger)'; if (strengthEl) { strengthEl.style.setProperty('--strength', pct + '%'); strengthEl.style.setProperty('background', 'var(--border)'); const after = document.createElement('style'); after.innerHTML = `#password-strength::after{background:${color}}`; document.head.appendChild(after);} return score; };

        tabs.forEach(tab => {
            tab.addEventListener('click', () => {
                tabs.forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                const tabType = tab.dataset.tab;
                if (tabType === 'login') { loginForm.style.display = 'block'; registerForm.style.display = 'none'; setSubmit('Sign In', false); }
                else { loginForm.style.display = 'none'; registerForm.style.display = 'block'; setSubmit('Create Account', false); }
                this.hideAuthError();
                // Re-ensure eye toggles remain in place after DOM flips
                ['login-password','register-password','register-password-confirm'].forEach(id => ensureEyeToggle(id));
                // Conditionally render Forgot Password link only if SMTP enabled
                const existing = document.getElementById('forgot-link'); if (existing) existing.remove();
                if (window.__SITE_EMAIL_ENABLED__ && tab.dataset.tab === 'login') {
                    const a = document.createElement('button'); a.id='forgot-link'; a.className='link-btn'; a.type='button'; a.textContent='Forgot password?'; a.style.alignSelf='flex-end'; a.onclick = () => this.openForgotPassword(); loginForm.appendChild(a);
                }
            });
        });

        // Live strength meter
        const registerPassword = document.getElementById('register-password'); if (registerPassword) registerPassword.addEventListener('input', (e) => renderStrength(e.target.value));

        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            const isLogin = document.querySelector('.auth-tab.active').dataset.tab === 'login';
            setSubmit(isLogin ? 'Signing In…' : 'Creating…', true);
            try { if (isLogin) { await this.handleLogin(); } else { await this.handleRegister(); } } finally { setSubmit(isLogin ? 'Sign In' : 'Create Account', false); }
        });

        // Initial eye toggles and forgot link
        ['login-password','register-password','register-password-confirm'].forEach(id => ensureEyeToggle(id));
        const existing = document.getElementById('forgot-link'); if (existing) existing.remove();
        if (window.__SITE_EMAIL_ENABLED__) {
            const a = document.createElement('button'); a.id='forgot-link'; a.className='link-btn'; a.type='button'; a.textContent='Forgot password?'; a.style.alignSelf='flex-end'; a.onclick = () => this.openForgotPassword(); document.getElementById('login-form').appendChild(a);
        }
    }

    async handleLogin() {
        const email = document.getElementById('login-email').value.trim();
        const password = document.getElementById('login-password').value;
        if (!email || !password) { this.showAuthError('Please fill in all fields'); return; }
        this.showLoader(); this.hideAuthError();
        try {
            const response = await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password }) });
            const data = await response.json().catch(() => ({}));
            if (response.ok) {
                localStorage.setItem('token', data.token);
                localStorage.setItem('user', JSON.stringify(data.user));
                this.currentUser = data.user;
                this.closeAuthModal(); this.updateAuthButton(); this.showNotification('Welcome back!', 'success');
            } else if (response.status === 403 && data.error && /verify/i.test(data.error)) {
                this.showAuthError('Email not verified. Please check your inbox.');
            } else {
                this.showAuthError(data.error || 'Login failed');
            }
        } catch (error) {
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
        // Ensure magnetic scroll disables while modal is open
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        
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
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
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
            // Styled in-app error view
            this.gallery.innerHTML = '';
            if (this.profileTop) this.profileTop.innerHTML = '';
            const wrap = document.createElement('section');
            wrap.className = 'mono-col';
            wrap.style.cssText = 'margin:120px auto 0;max-width:720px;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--surface-elevated);color:var(--text-primary)';
            wrap.innerHTML = `
              <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
                <div style="font-weight:800;letter-spacing:-0.02em">User not found</div>
                <span style="opacity:.6;font-family:var(--font-mono);font-size:12px">error: profile_missing</span>
              </div>
              <div style="color:var(--text-secondary);font-family:var(--font-mono);line-height:1.6">The profile <strong>@${username}</strong> does not exist.</div>
              <div style="margin-top:12px"><a href="/" class="nav-btn" style="text-decoration:none">Back to river</a></div>
            `;
            this.gallery.appendChild(wrap);
            return;
        }
        const isOwner = this.currentUser && (this.currentUser.username === user.username);
        const isAdmin = !!this.currentUser?.is_admin;
        const isModerator = !!this.currentUser?.is_moderator;

        // Normalize image objects (ensure caption field exists as string)
        imgs.images = (imgs.images || []).map(img => ({
            ...img,
            caption: typeof img.caption === 'string' ? img.caption : (img.caption || '')
        }));

        // Header (no backdrop)
        const header = document.createElement('section');
        header.className = 'mono-col';
        header.style.cssText = 'padding:16px 0;color:var(--text-primary);display:flex;align-items:center;justify-content:space-between;gap:12px;margin:0 auto;position:relative';
        const avatar = `<div class="avatar-preview" style="background-image:url('${user.avatar_url || ''}');"></div>`;
        const adminBtn = (isOwner && (isAdmin || isModerator)) ? '<button id="profile-admin" class="link-btn">Admin</button>' : '';
        header.innerHTML = `
          <div style="display:flex;gap:12px;align-items:center">
            ${avatar}
            <div style="font-weight:700;font-size:1.1rem;font-family:var(--font-mono)">@${user.username}</div>
          </div>
          ${isOwner ? `
          <div class="profile-actions" style="display:flex;gap:8px;align-items:center">
            <div class="profile-actions-inline" style="display:flex;gap:8px;align-items:center">
              <button id="profile-logout" class="nav-btn">Sign out</button>
              ${adminBtn}
              <button id="profile-settings" class="link-btn">Settings</button>
            </div>
            <div class="profile-actions-menu" style="display:none;position:relative">
              <button id="profile-actions-toggle" class="nav-btn">Options</button>
              <div id="profile-actions-panel" class="profile-menu" style="display:none;position:absolute;right:0;top:calc(100% + 8px);min-width:180px;background:var(--surface-elevated);border:1px solid var(--border);border-radius:10px;padding:6px;box-shadow:var(--shadow-2xl);z-index:50">
                <button id="menu-settings" class="profile-item link-btn" style="display:block;width:100%;text-align:left;padding:8px 10px">Settings</button>
                ${(isAdmin || isModerator) ? '<button id="menu-admin" class="profile-item link-btn" style="display:block;width:100%;text-align:left;padding:8px 10px">Admin</button>' : ''}
                <button id="menu-signout" class="profile-item link-btn" style="display:block;width:100%;text-align:left;padding:8px 10px;color:#ff6666">Sign out</button>
              </div>
            </div>
          </div>` : ''}
        `;
        this.profileTop.appendChild(header);
        if (isOwner) {
            const sBtn = document.getElementById('profile-settings'); if (sBtn) sBtn.onclick = () => { history.pushState({}, '', '/settings'); this.renderSettingsPage(); };
            const aBtn = document.getElementById('profile-admin'); if (aBtn) aBtn.onclick = () => { history.pushState({}, '', '/admin'); this.renderAdminPage(); };
            const logoutBtn = document.getElementById('profile-logout'); if (logoutBtn) logoutBtn.onclick = () => { this.signOut(); window.location.href = '/'; };
            // Mobile menu wiring
            const toggle = document.getElementById('profile-actions-toggle');
            const panel = document.getElementById('profile-actions-panel');
            const openPanel = () => { if (panel) panel.style.display = 'block'; };
            const closePanel = () => { if (panel) panel.style.display = 'none'; };
            if (toggle && panel) {
                toggle.onclick = (e) => { e.stopPropagation(); panel.style.display = (panel.style.display === 'block') ? 'none' : 'block'; };
                document.addEventListener('click', (e) => { if (!panel.contains(e.target) && e.target !== toggle) closePanel(); });
            }
            const mSettings = document.getElementById('menu-settings'); if (mSettings) mSettings.onclick = () => { closePanel(); history.pushState({}, '', '/settings'); this.renderSettingsPage(); };
            const mAdmin = document.getElementById('menu-admin'); if (mAdmin) mAdmin.onclick = () => { closePanel(); history.pushState({}, '', '/admin'); this.renderAdminPage(); };
            const mSign = document.getElementById('menu-signout'); if (mSign) mSign.onclick = () => { closePanel(); this.signOut(); window.location.href = '/'; };
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
                for (const f of files) {
                    const uploaded = await this.uploadImage(f, {});
                    if (uploaded) {
                        this.openEditModal({ id: uploaded.id, original_name: uploaded.original_name, caption: uploaded.caption || '', is_nsfw: false, filename: uploaded.filename }, null);
                    }
                }
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
                  <div style="display:flex;justify-content:space-between;align-items:center;margin-top:6px;margin-bottom:9px">
                    <div style="display:flex;gap:10px;align-items:center">
                      <small id="bio-count" style="color:var(--text-secondary)"></small>
                      <small style="color:var(--text-tertiary)">Markdown supported: <strong>bold</strong>, <em>italic</em>, [link](https://)</small>
                    </div>
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
            const token = localStorage.getItem('token');
            const resp = await fetch(`/api/feed?page=${this.page}`, {
                headers: token ? { 'Authorization': `Bearer ${token}` } : undefined
            });
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
        const isModerator = !!this.currentUser && !!this.currentUser.is_moderator;
        const canEdit = (onProfile && isOwner) || isAdmin || isModerator;

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
            img.src = this.getImageURL(image.filename);
            img.alt = image.original_name || image.title || '';
            img.loading = 'lazy';
            // NSFW blur logic based on current user preference
            const nsfwPref = (this.currentUser?.nsfw_pref || (this.currentUser?.show_nsfw ? 'show' : 'hide'));
            const shouldBlur = image.is_nsfw && nsfwPref === 'blur';
            const shouldHide = image.is_nsfw && (!this.currentUser || nsfwPref === 'hide');
            if (shouldHide) { return; }
            if (shouldBlur) {
                // Simple approach: just add the blur class and image
                card.classList.add('nsfw-blurred');
                card.appendChild(img);
                
                // Store state directly on the card element
                card._nsfwRevealed = false;
                
                // Don't add 'hovering' class - let normal hover effects work
            } else {
                card.appendChild(img);
            }

            const meta = document.createElement('div');
            meta.className = 'image-meta';
            const username = image.username || image.author || 'Unknown';
            const captionHtml = image.caption ? `<div class="image-caption" style="margin-top:4px;color:var(--text-secondary);font-size:0.8rem">${this.sanitizeAndRenderMarkdown(image.caption)}</div>` : '';
            const actions = canEdit ? `
                <div class="image-actions" style="display:flex;gap:2px;align-items:center;flex-shrink:0">
                  <button title="Edit" class="like-btn" data-act="edit" data-id="${image.id}" style="width:28px;height:28px;padding:0;color:var(--text-secondary)">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 1 1 3 3L7 19l-4 1 1-4Z"/></svg>
                  </button>
                  <button title="Delete" class="like-btn" data-act="delete" data-id="${image.id}" style="width:28px;height:28px;padding:0;color:#ff6666">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </div>` : '';
            meta.innerHTML = `
                <div style="display:flex;align-items:center;justify-content:space-between;gap:12px">
                  <div style="min-width:0">
                    <div class="image-title" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis"><a href="/i/${encodeURIComponent(image.id)}" class="image-link" style="color:inherit;text-decoration:none">${(image.title || image.original_name || 'Untitled').trim()}</a></div>
                    <div class="image-author" style="font-family:var(--font-mono)"><a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${username}</a></div>
                  </div>
                  ${actions}
                </div>
                ${captionHtml}`;
            meta.addEventListener('click', async (e) => {
                const a = e.target.closest('a.image-link');
                if (a) {
                    e.preventDefault();
                    e.stopPropagation();
                    history.pushState({}, '', a.getAttribute('href'));
                    await this.renderImagePage(image.id);
                    return;
                }
                const btn = e.target.closest('button');
                if (!btn) return;
                const act = btn.dataset.act;
                const id = btn.dataset.id;
                e.stopPropagation();
                if (act === 'delete') {
                    const ok = await this.showConfirm('Delete image?');
                    if (ok) {
                        const resp = await fetch(`/api/images/${id}`, { method: 'DELETE', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }});
                        if (resp.status === 204) { card.remove(); this.showNotification('Image deleted'); } else { this.showNotification('Delete failed', 'error'); }
                    }
                } else if (act === 'edit') {
                    this.openEditModal(image, card);
                }
            });
            card.appendChild(meta);

            // Hover effect classes to drive keyframed filter flash
            const restartAnim = () => { img.style.animation = 'none'; void img.offsetWidth; img.style.animation = ''; };
            card.addEventListener('mouseenter', () => {
                restartAnim();
                card.classList.remove('leaving');
                card.classList.add('hovering');
            });
            card.addEventListener('mouseleave', () => {
                restartAnim();
                card.classList.remove('hovering');
                card.classList.add('leaving');
                // Clean up class after animation ends
                img.addEventListener('animationend', function onEnd() {
                    card.classList.remove('leaving');
                    img.removeEventListener('animationend', onEnd);
                });
            });

            // Toggle caption expansion on first image click (default view clamps)
            if (image.caption) {
                let captionExpanded = false;
                const toggleCaption = (ev) => {
                    ev.stopPropagation();
                    const cap = meta.querySelector('.image-caption');
                    if (!cap) return;
                    cap.classList.toggle('expanded');
                    captionExpanded = cap.classList.contains('expanded');
                };
                img.addEventListener('click', (ev) => { if (!captionExpanded) toggleCaption(ev); });
                // Also allow toggling by clicking the caption itself
                meta.addEventListener('click', (ev) => {
                    const capEl = ev.target.closest('.image-caption');
                    if (capEl) { toggleCaption(ev); }
                });
            }
        }

        card.addEventListener('click', (e) => {
            // Handle NSFW blur logic - single click removes blur immediately
            if (card.classList.contains('nsfw-blurred') && !card._nsfwRevealed) {
                // Single click: start reveal animation and immediately allow lightbox
                card._nsfwRevealed = true;
                card.classList.add('revealing');
                
                // After melting animation completes, clean up classes
                setTimeout(() => {
                    card.classList.remove('nsfw-blurred', 'revealing');
                    card.classList.add('nsfw-revealed');
                }, 1200); // Match the CSS animation duration
                
                // Don't stop propagation - let it fall through to open lightbox
                // This creates the desired behavior: single click reveals AND opens lightbox
            }
            
            // Open lightbox (happens for all clicks)
            this.openLightbox(image);
        });
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
            ${filename ? `<div style="display:flex;justify-content:center;"><img src="${this.getImageURL(filename)}" alt="" style="max-height:60vh;width:auto;border-radius:10px;border:1px solid var(--border);margin-bottom:12px"/></div>` : ''}
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
        const lightboxCaption = document.getElementById('lightbox-caption');
        if (!lightboxImg) return;

        if (image.filename) {
            lightboxImg.src = this.getImageURL(image.filename);
            lightboxImg.alt = image.original_name || image.title || '';
        }
        const username = image.username || image.author || 'Unknown';
        const titleText = image.title || image.original_name || 'Untitled';
        // Title becomes a link to the single-image page
        lightboxTitle.innerHTML = `<a href="/i/${encodeURIComponent(image.id)}" class="image-link" style="color:inherit;text-decoration:none">${titleText}</a>`;
        const link = lightboxTitle.querySelector('a.image-link');
        if (link) {
            link.addEventListener('click', async (e) => {
                e.preventDefault();
                history.pushState({}, '', link.getAttribute('href'));
                this.closeLightbox();
                await this.renderImagePage(image.id);
            });
        }
        lightboxAuthor.innerHTML = `<a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${username}</a>`;
        lightboxAuthor.style.fontFamily = 'var(--font-mono)';
        lightboxLike.classList.remove('liked');
        lightboxLike.onclick = () => this.toggleLike(image.id);
        // Render caption (sanitized markdown) in lightbox
        if (lightboxCaption) {
            lightboxCaption.innerHTML = image.caption ? this.sanitizeAndRenderMarkdown(image.caption) : '';
        }
        this.lightbox.classList.add('active');
        document.body.style.overflow = 'hidden';
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
    }

    closeLightbox() {
        this.lightbox.classList.remove('active');
        document.body.style.overflow = '';
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
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
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        if (!this.currentUser) { this.showAuthModal(); return; }
        let email = '';
        try { const resp = await fetch('/api/me/account', { headers: { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}` } }); if (resp.ok) { const acc = await resp.json(); email = acc.email || ''; } } catch {}

        this.gallery.innerHTML = '';
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.classList.add('settings-mode');
        const wrap = document.createElement('div');
        wrap.className = 'settings-wrap';
        const avatarURL = (this.currentUser && this.currentUser.avatar_url) ? this.currentUser.avatar_url : '';
        const needVerify = !!window.__SITE_EMAIL_ENABLED__ && this.currentUser && this.currentUser.email_verified === false;
        wrap.innerHTML = `
          <section class="settings-group">
            <div class="settings-label">Profile</div>
            <div class="avatar-row" style="align-items:flex-start;gap:12px">
              <div class="avatar-preview" id="avatar-preview" style="background-image:url('${avatarURL}')"></div>
              <div style="display:grid;gap:10px;flex:1;min-width:0;overflow:hidden">
                <label class="settings-label">Username</label>
                <input type="text" id="settings-username" value="${this.currentUser.username}" minlength="3" maxlength="30" class="settings-input"/>
                <div class="settings-actions"><button id="btn-username" class="nav-btn">Change Username</button></div>
                <label class="settings-label">Avatar</label>
                <div class="settings-actions" style="gap:8px;align-items:center;min-width:0"><input type="file" id="avatar-file" accept="image/*" style="min-width:0"/><button id="avatar-upload" class="nav-btn">Upload</button></div>
                <label class="settings-label">Email</label>
                <input type="email" id="settings-email" value="${email}" class="settings-input"/>
                <div class="settings-actions">
                  <button id="btn-email" class="nav-btn">Save Email</button>
                  ${needVerify ? '<button id="btn-resend-verify" class="nav-btn">Resend verification</button>' : ''}
                </div>
                <label class="settings-label">Password</label>
                <input type="password" id="current-password" placeholder="Current password" class="settings-input"/>
                <input type="password" id="new-password" placeholder="New password" minlength="6" class="settings-input"/>
                <input type="password" id="new-password-confirm" placeholder="Confirm new password" minlength="6" class="settings-input"/>
                <div id="pw-strength" style="height:6px;width:100%;background:var(--border);border-radius:999px;overflow:hidden"><div id="pw-bar" style="height:6px;width:0;background:var(--color-danger)"></div></div>
                <div class="settings-actions"><button id="btn-password" class="nav-btn">Change Password</button></div>
                <label class="settings-label">NSFW content</label>
                <div style="display:flex;gap:8px;flex-wrap:wrap">
                  <label style="display:flex;gap:6px;align-items:center"><input type="radio" name="nsfw-pref" value="hide"> Hide</label>
                  <label style="display:flex;gap:6px;align-items:center"><input type="radio" name="nsfw-pref" value="show"> Show</label>
                  <label style="display:flex;gap:6px;align-items:center"><input type="radio" name="nsfw-pref" value="blur"> Blur until clicked</label>
                </div>
                <div class="settings-actions"><button id="btn-nsfw" class="nav-btn">Save NSFW preference</button></div>
              </div>
            </div>
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
        const scorePassword = (pwd) => { let s = 0; if (!pwd) return 0; if (pwd.length >= 8) s++; if (/[A-Z]/.test(pwd)) s++; if (/[a-z]/.test(pwd)) s++; if (/[0-9]/.test(pwd)) s++; if (/[^A-Za-z0-9]/.test(pwd)) s++; return Math.min(s,5); };
        const renderBar = () => { const score = scorePassword(pw.value); const pct = [0,20,40,60,80,100][score]; const ok = score>=4; bar.style.width = pct+'%'; bar.style.background = ok ? 'var(--color-ok)' : score>=3 ? 'var(--color-warn)' : 'var(--color-danger)'; };
        pw.addEventListener('input', renderBar); renderBar();

        // Back navigation remains unchanged
        window.onpopstate = () => {
            if (location.pathname.startsWith('/@')) {
                this.gallery.classList.remove('settings-mode');
                const u = decodeURIComponent(location.pathname.slice(2));
                this.renderProfilePage(u);
            } else {
                this.gallery.classList.remove('settings-mode');
                this.gallery.innerHTML = ''; if (this.profileTop) this.profileTop.innerHTML=''; this.page=1; this.hasMore=true; this.loadImages();
            }
            if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        };

        // Handlers remain (updated references)
        const authHeader = { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}`, 'Content-Type': 'application/json' };
        const pref = (this.currentUser?.nsfw_pref || ((this.currentUser?.show_nsfw) ? 'show' : 'hide'));
        (document.querySelector(`input[name='nsfw-pref'][value='${pref}']`)||document.querySelector(`input[name='nsfw-pref'][value='hide']`)).checked = true;
        document.getElementById('btn-nsfw').onclick = async () => {
            const sel = document.querySelector("input[name='nsfw-pref']:checked")?.value || 'hide';
            try { const resp = await fetch('/api/me/profile', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ nsfw_pref: sel }) }); if (!resp.ok) throw await resp.json(); const u = await resp.json(); this.currentUser = u; localStorage.setItem('user', JSON.stringify(u)); this.showNotification('NSFW preference saved'); } catch (e) { document.getElementById('err-nsfw').textContent = e.error || 'Failed'; }
        };
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
            try {
                const resp = await fetch('/api/me/avatar', { method:'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token') || ''}` }, body: fd });
                if (!resp.ok) throw await resp.json();
                const data = await resp.json();
                const pv = document.getElementById('avatar-preview'); if (pv) pv.style.backgroundImage = `url('${data.avatar_url}')`;
                this.currentUser.avatar_url = data.avatar_url; localStorage.setItem('user', JSON.stringify(this.currentUser));
                // Also update profile header avatar if present on page
                const headerAv = document.querySelector('.avatar-preview'); if (headerAv) headerAv.style.backgroundImage = `url('${data.avatar_url}')`;
                this.showNotification('Avatar updated');
            } catch (e) { this.showNotification(e.error || 'Upload failed', 'error'); }
        };
        if (needVerify) {
            const btn = document.getElementById('btn-resend-verify');
            if (btn) btn.onclick = async () => {
                try { const r = await fetch('/api/me/resend-verification', { method:'POST', headers:{ 'Authorization': `Bearer ${localStorage.getItem('token')}` } }); if (r.status===204) this.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Unable to send','error'); } } catch {}
            };
        }
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
            } else if (response.status === 400) {
                const error = await response.json().catch(() => ({}));
                await this.showErrorModal('Upload rejected', error.error || 'Only AI images with verifiable metadata (EXIF/XMP/C2PA) are accepted.');
            } else if (response.status === 401) {
                localStorage.removeItem('token');
                localStorage.removeItem('user');
                this.currentUser = null;
                this.checkAuth();
                this.showAuthModal();
            } else {
                const error = await response.json().catch(() => ({}));
                await this.showErrorModal('Upload failed', error.error || 'Unknown error');
            }
        } catch (error) {
            await this.showErrorModal('Upload failed', (error && error.message) || 'Network error');
        } finally {
            this.hideLoader();
        }
        return null;
    }

    async showErrorModal(title, message) {
        return new Promise((resolve) => {
            const overlay = document.createElement('div');
            overlay.style.cssText = 'position:fixed;inset:0;z-index:3000;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
            const panel = document.createElement('div');
            panel.style.cssText = 'max-width:520px;width:100%;background:var(--surface-elevated);border:1px solid var(--border-strong);border-radius:12px;padding:16px;color:var(--text-primary);box-shadow:0 20px 60px rgba(0,0,0,0.45)';
            panel.innerHTML = `
                <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">
                  <span style="display:inline-flex;width:28px;height:28px;border-radius:50%;align-items:center;justify-content:center;background:#ef44441a;border:1px solid #ef444433;color:#ef4444">!</span>
                  <div style="font-weight:700">${this.escapeHTML(String(title||'Error'))}</div>
                </div>
                <div style="color:var(--text-secondary);font-family:var(--font-mono);white-space:pre-wrap;margin-bottom:12px">${this.escapeHTML(String(message||''))}</div>
                <div style="color:var(--text-tertiary);font-size:12px;line-height:1.5;margin:-4px 0 12px">
                  We’re actively tuning our filters. If this seems wrong, please email details to ${this.escapeHTML(window.__SITE_FROM_EMAIL__||'our support email')}.
                </div>
                <div style="display:flex;gap:8px;justify-content:flex-end">
                  <button id="err-ok" class="nav-btn">OK</button>
                </div>`;
            overlay.appendChild(panel);
            const close = () => { overlay.remove(); resolve(); };
            panel.querySelector('#err-ok').onclick = close;
            overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });
            document.addEventListener('keydown', function onKey(e){ if(e.key==='Escape'){ close(); document.removeEventListener('keydown', onKey);} });
            document.body.appendChild(overlay);
        });
    }

    escapeHTML(s){ return (s||'').replace(/[&<>"]/g, c=>({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;' }[c])); }

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

    // MOBILE-ONLY: Magnetic kinetic scroll that snaps to each image card on low-velocity stops
    setupMobileMagneticScroll() {
        this._evaluateMagnetEnabled();
        this._computeMagnetOffset();

        if (!this.magneticListenersAttached) {
            this.magneticListenersAttached = true;
            // Track touch velocity
            window.addEventListener('touchstart', (e) => {
                this._evaluateMagnetEnabled();
                if (!this.magneticEnabled) return;
                const t = e.touches && e.touches[0];
                if (!t) return;
                this._lastTouchY = t.clientY;
                this._lastTouchTime = performance.now();
                this.magnetMaxVelocity = 0;
            }, { passive: true });
            window.addEventListener('touchmove', (e) => {
                this._evaluateMagnetEnabled();
                if (!this.magneticEnabled) return;
                const t = e.touches && e.touches[0];
                if (!t) return;
                const now = performance.now();
                const dy = t.clientY - this._lastTouchY;
                const dt = now - this._lastTouchTime;
                if (dt > 0) {
                    const v = Math.abs(dy / dt); // px/ms
                    if (v > this.magnetMaxVelocity) this.magnetMaxVelocity = v;
                }
                this._lastTouchY = t.clientY;
                this._lastTouchTime = now;
            }, { passive: true });
            window.addEventListener('touchend', () => {
                this._evaluateMagnetEnabled();
                if (!this.magneticEnabled) return;
                // Fast flick? Skip the next snap
                if (this.magnetMaxVelocity >= this.magnetVelocityThreshold) {
                    this.magnetSkipNextSnap = true;
                } else {
                    // Snap very soon after release for quicker response, but give inertia a tick
                    setTimeout(() => { if (!this._activeScrollAnim) this._snapToNearestCard(); }, 20);
                }
            }, { passive: true });

            // Debounced snap after scrolling idles
            window.addEventListener('scroll', () => {
                this._evaluateMagnetEnabled();
                if (!this.magneticEnabled) return;
                // Track direction by scroll delta
                const y = window.scrollY;
                const dy = y - this._lastScrollY;
                if (Math.abs(dy) > 0.5) this._magnetDirection = dy > 0 ? 1 : -1;
                this._lastScrollY = y;
                this._scheduleMagnetSnap();
            }, { passive: true });

            // Re-evaluate on resize/orientation
            window.addEventListener('resize', () => { this._evaluateMagnetEnabled(); this._computeMagnetOffset(); });
            window.addEventListener('orientationchange', () => { this._evaluateMagnetEnabled(); this._computeMagnetOffset(); });
        }
    }

    _scheduleMagnetSnap() {
        if (this._magnetSnapTimer) clearTimeout(this._magnetSnapTimer);
        if (this._activeScrollAnim) return; // avoid fighting active animation
        // If a fast flick happened, consume the skip once then return
        if (this.magnetSkipNextSnap) {
            this.magnetSkipNextSnap = false;
            return;
        }
        this._magnetSnapTimer = setTimeout(() => this._snapToNearestCard(), 35);
    }

    _snapToNearestCard() {
        if (!this.magneticEnabled) return;
        // Do not interfere with modals/lightbox
        if (document.body.style.overflow === 'hidden') return;
        const cards = Array.from(document.querySelectorAll('.image-card'));
        if (cards.length === 0) return;

        const sel = this._selectSnapTarget(cards);
        if (!sel) return;
        const { node: bestEl, snapY } = sel;
        let snapTo = snapY;
        const maxScroll = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
        if (snapTo < 0) snapTo = 0; else if (snapTo > maxScroll) snapTo = maxScroll;
        if (Math.abs(window.scrollY - snapTo) < 1) return;
        if (this._lastSnapEl === bestEl && Math.abs(this._lastSnapY - snapTo) < 2) return; // stable target, skip

        // Luxe animation: dynamic duration based on distance and easing
        const distance = Math.abs(window.scrollY - snapTo);
        const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        const duration = prefersReduced ? 0 : Math.min(820, Math.max(260, 260 + distance * 0.32));
        this._animateScrollTo(snapTo, duration);

        // Highlight the focused card container
        const focusedCard = bestEl.closest ? (bestEl.closest('.image-card') || bestEl) : bestEl;
        document.querySelectorAll('.image-card.focused').forEach(el => el.classList.remove('focused'));
        if (focusedCard) focusedCard.classList.add('focused');
        this._lastSnapEl = bestEl;
        this._lastSnapY = snapTo;
    }

    _animateScrollTo(targetY, durationMs = 400) {
        if (this._activeScrollAnim) { this._activeScrollAnim.cancelled = true; this._activeScrollAnim = null; }
        if (durationMs <= 0) { window.scrollTo(0, targetY); return; }
        const startY = window.scrollY;
        const delta = targetY - startY;
        const startTime = performance.now();
        const anim = { cancelled: false };
        this._activeScrollAnim = anim;
        // Smooth blend between ease-out-sine at start and ease-out-cubic at end
        const easeOutSine = (t) => Math.sin((t * Math.PI) / 2);
        const easeOutCubic = (t) => 1 - Math.pow(1 - t, 3);
        const ease = (t) => {
            const mid = 0.35; // earlier acceleration, longer gentle settle
            if (t < mid) {
                const nt = t / mid;
                return easeOutSine(nt) * mid;
            } else {
                const nt = (t - mid) / (1 - mid);
                return mid + easeOutCubic(nt) * (1 - mid);
            }
        };
        const step = () => {
            if (anim.cancelled) return;
            const now = performance.now();
            const t = Math.min(1, (now - startTime) / durationMs);
            const y = startY + delta * ease(t);
            window.scrollTo(0, Math.round(y));
            if (t < 1) requestAnimationFrame(step);
            else this._activeScrollAnim = null;
        };
        requestAnimationFrame(step);
        // Cancel animation on user input
        const cancelOnInput = () => { if (this._activeScrollAnim) this._activeScrollAnim.cancelled = true; this._activeScrollAnim = null; cleanup(); };
        const cleanup = () => {
            window.removeEventListener('wheel', cancelOnInput, { passive: true });
            window.removeEventListener('touchstart', cancelOnInput, { passive: true });
            window.removeEventListener('keydown', cancelOnInput);
        };
        window.addEventListener('wheel', cancelOnInput, { passive: true });
        window.addEventListener('touchstart', cancelOnInput, { passive: true });
        window.addEventListener('keydown', cancelOnInput);
    }

    _selectSnapTarget(cards) {
        const viewportTop = window.scrollY;
        const viewportBottom = viewportTop + window.innerHeight;
        const viewportCenter = viewportTop + (window.innerHeight / 2);
        const dir = this._magnetDirection >= 0 ? 1 : -1;

        const candidates = [];
        for (const card of cards) {
            const node = card.querySelector('img') || card;
            const r = node.getBoundingClientRect();
            if (r.height <= 0) continue;
            const elTop = r.top + window.scrollY;
            const elBottom = elTop + r.height;
            const elCenter = elTop + r.height / 2;
            const overlapPx = Math.max(0, Math.min(viewportBottom, elBottom) - Math.max(viewportTop, elTop));
            const viewportOverlapRatio = overlapPx / window.innerHeight;
            const elementOverlapRatio = overlapPx / Math.max(1, r.height);
            const hasExpandedCaption = !!card.querySelector('.image-caption.expanded');
            const captionHeight = (() => { const cap = card.querySelector('.image-caption'); return cap ? cap.getBoundingClientRect().height : 0; })();
            const snapDisabled = hasExpandedCaption && captionHeight > window.innerHeight * 0.45; // disable if caption dominates view
            candidates.push({ node, elTop, elBottom, elCenter, viewportOverlapRatio, elementOverlapRatio });
        }
        if (!candidates.length) return null;

        // If a tall or currently dominant element occupies most of the viewport, favor it
        const dominant = candidates.find(c => c.viewportOverlapRatio >= 0.6 || c.elementOverlapRatio >= 0.7);
        if (dominant) {
            const snapY = Math.round(dominant.elCenter - (window.innerHeight / 2));
            return { node: dominant.node, snapY };
        }

        // Direction-aware: pick nearest in the scroll direction based on edge proximity
        if (dir > 0) {
            // Down: find the first element whose top is at or below current top (with small epsilon)
            const forward = candidates
                .filter(c => c.elTop >= (viewportTop - this._epsilonBackward))
                .sort((a, b) => (a.elTop - viewportTop) - (b.elTop - viewportTop));
            let chosen = forward.find(c => !((c.snapDisabled===true))) || forward[0];
            if (!chosen) chosen = candidates.sort((a,b)=>Math.abs(a.elCenter-viewportCenter)-Math.abs(b.elCenter-viewportCenter))[0];
            const snapY = Math.round(chosen.elCenter - (window.innerHeight / 2));
            return { node: chosen.node, snapY };
        } else {
            // Up: find the last element whose bottom is at or above current bottom (with small epsilon)
            const backward = candidates
                .filter(c => c.elBottom <= (viewportBottom + this._epsilonBackward))
                .sort((a, b) => (viewportBottom - b.elBottom) - (viewportBottom - a.elBottom));
            let chosen = backward.find(c => !((c.snapDisabled===true))) || backward[0];
            if (!chosen) chosen = candidates.sort((a,b)=>Math.abs(a.elCenter-viewportCenter)-Math.abs(b.elCenter-viewportCenter))[0];
            const snapY = Math.round(chosen.elCenter - (window.innerHeight / 2));
            return { node: chosen.node, snapY };
        }
    }

    _evaluateMagnetEnabled() {
        const isMobile = window.matchMedia('(max-width: 600px)').matches;
        const inSettings = this.gallery?.classList?.contains('settings-mode');
        const path = location.pathname || '/';
        const blocked = (path === '/settings' || path === '/admin' || path === '/reset' || path === '/verify');
        this.magneticEnabled = isMobile && !blocked && !inSettings;
    }

    _computeMagnetOffset() {
        // Try to keep snapped card just below the fixed nav
        const nav = document.getElementById('nav');
        const navH = nav ? nav.getBoundingClientRect().height : 0;
        // A small breathing space below nav
        const gap = 12;
        this.magnetOffset = Math.max(0, Math.round(navH + gap));
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

    // Minimal in-app confirm modal
    showConfirm(message) {
        return new Promise((resolve) => {
            const overlay = document.createElement('div');
            overlay.style.cssText = 'position:fixed;inset:0;z-index:2800;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
            const panel = document.createElement('div');
            panel.style.cssText = 'max-width:460px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
            panel.innerHTML = `
                <div style="font-weight:700;margin-bottom:8px">Confirm</div>
                <div style="color:var(--text-secondary);font-family:var(--font-mono);margin-bottom:12px">${message}</div>
                <div style="display:flex;gap:8px;justify-content:flex-end">
                  <button id="cf-cancel" class="nav-btn">Cancel</button>
                  <button id="cf-ok" class="nav-btn nav-btn-danger">Delete</button>
                </div>`;
            overlay.appendChild(panel);
            const done = (val) => { overlay.remove(); resolve(val); };
            panel.querySelector('#cf-cancel').onclick = () => done(false);
            panel.querySelector('#cf-ok').onclick = () => done(true);
            overlay.addEventListener('click', (e) => { if (e.target === overlay) done(false); });
            document.addEventListener('keydown', function onKey(e){ if(e.key==='Escape'){ done(false); document.removeEventListener('keydown', onKey);} });
            document.body.appendChild(overlay);
        });
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

    async renderAdminPage() {
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        if (!this.currentUser) { this.showAuthModal(); return; }
        const isAdmin = !!this.currentUser.is_admin;
        const isModerator = !!this.currentUser.is_moderator;
        if (!isAdmin && !isModerator) { this.showNotification('Forbidden', 'error'); history.replaceState({}, '', `/@${encodeURIComponent(this.currentUser.username)}`); return; }

        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.innerHTML = '';
        this.gallery.classList.add('settings-mode');

        const wrap = document.createElement('div');
        wrap.className = 'settings-wrap';

        const siteSection = document.createElement('section');
        siteSection.className = 'settings-group';
        if (isAdmin) {
            let s = {};
            try { const r = await fetch('/api/admin/site', { headers: { 'Authorization': `Bearer ${localStorage.getItem('token')||''}` }}); if (r.ok) s = await r.json(); } catch {}
            const smtpConfigured = !!(s.smtp_host && s.smtp_port && s.smtp_username && s.smtp_password);
            siteSection.innerHTML = `
              <div class="settings-label">Site settings</div>
              <input id="site-name" class="settings-input" placeholder="Site name" value="${s.site_name||''}"/>
              <input id="site-url" class="settings-input" placeholder="Site URL" value="${s.site_url||''}"/>
              <input id="seo-title" class="settings-input" placeholder="SEO title" value="${s.seo_title||''}"/>
              <textarea id="seo-description" class="settings-input" placeholder="SEO description">${s.seo_description||''}</textarea>
              <div class="settings-label">Social image</div>
              <input id="social-image" class="settings-input" placeholder="Social image URL" value="${s.social_image_url||''}"/>
              <div class="settings-actions" style="gap:8px;align-items:center">
                <input id="social-image-file" type="file" accept="image/*"/>
                <button id="btn-upload-social" class="nav-btn">Upload social image</button>
                <img id="social-image-preview" src="${s.social_image_url||''}" alt="Social image preview" style="height:40px;aspect-ratio:1/1;object-fit:cover;border:1px solid var(--border);border-radius:8px;${s.social_image_url?'':'display:none'}"/>
              </div>
              <div class="settings-label">Storage</div>
              <div style="display:grid;gap:8px">
                <label class="settings-label">Provider</label>
                <select id="storage-provider" class="settings-input">
                  <option value="local" ${!s.storage_provider || s.storage_provider==='local' ? 'selected' : ''}>Local</option>
                  <option value="s3" ${s.storage_provider==='s3' || s.storage_provider==='r2' ? 'selected' : ''}>S3 / R2</option>
                </select>
                <input id="s3-endpoint" class="settings-input" placeholder="S3/R2 endpoint (https://...)" value="${s.s3_endpoint||''}"/>
                <input id="s3-bucket" class="settings-input" placeholder="Bucket name" value="${s.s3_bucket||''}"/>
                <input id="s3-access" class="settings-input" placeholder="Access key" value="${s.s3_access_key||''}"/>
                <input id="s3-secret" class="settings-input" type="password" placeholder="Secret key" value="${s.s3_secret_key||''}"/>
                <label style="display:flex;gap:8px;align-items:center"><input id="s3-path" type="checkbox" ${s.s3_force_path_style?'checked':''}/> Force path-style URLs</label>
                <input id="public-base" class="settings-input" placeholder="Public base URL (e.g., CDN)" value="${s.public_base_url||''}"/>
                <div class="settings-actions" style="gap:8px;align-items:center">
                  <span id="storage-status" class="meta" style="opacity:.8">Current: ${s.storage_provider||'local'}</span>
                  <button id="btn-test-storage" class="nav-btn">Verify storage</button>
                </div>
              </div>
              <div class="settings-actions" style="margin-top:8px;gap:8px;align-items:center">
                <button id="btn-save-site-top" class="nav-btn">Save</button>
                <button id="btn-export-upload" class="nav-btn">Migrate to Remote Storage</button>
              </div>
              <div class="settings-label">SMTP</div>
              <input id="smtp-host" class="settings-input" placeholder="SMTP host (hostname only, no http/https)" value="${s.smtp_host||''}"/>
              <input id="smtp-port" class="settings-input no-spinner" type="number" placeholder="SMTP port" value="${s.smtp_port||''}"/>
              <input id="smtp-username" class="settings-input" placeholder="SMTP username (often your full email address)" value="${s.smtp_username||''}"/>
              <input id="smtp-password" class="settings-input" type="password" placeholder="SMTP password" value="${s.smtp_password||''}"/>
              <input id="smtp-from" class="settings-input" placeholder="From email (optional, defaults to username)" value="${s.smtp_from_email||''}"/>
              <label style="display:flex;gap:8px;align-items:center"><input id="smtp-tls" type="checkbox" ${s.smtp_tls?'checked':''}/> Use TLS (465 implicit TLS or 587 STARTTLS)</label>
              ${smtpConfigured ? `<label style="display:flex;gap:8px;align-items:center"><input id="require-verify" type="checkbox" ${s.require_email_verification?'checked':''}/> Require email verification for new accounts</label>
              <div class="settings-actions" style="gap:8px;align-items:center"><input id="smtp-test-to" class="settings-input" placeholder="Test email to"/><button id="btn-smtp-test" class="nav-btn">Send test</button></div>` : '<small style="color:var(--text-tertiary)">Enter SMTP settings to enable email features</small>'}
              <div class="settings-actions"><button id="btn-save-site" class="nav-btn">Save</button></div>
              <div class="settings-label">Favicon</div>
              <div class="settings-actions" style="gap:8px;align-items:center">
                <input id="favicon-file" type="file" accept="image/x-icon,image/vnd.microsoft.icon,image/png,image/svg+xml"/>
                <button id="btn-upload-favicon" class="nav-btn">Upload favicon</button>
                <img id="favicon-preview" src="${s.favicon_path||''}" alt="Favicon preview" style="height:24px;width:24px;object-fit:contain;border:1px solid var(--border);border-radius:4px;${s.favicon_path?'':'display:none'}"/>
              </div>`;
            // Event handlers will be wired up in the isAdmin block below

            // Storage test handler will be wired up in the isAdmin block below

            const favInput = document.getElementById('favicon-file');
            const favPreview = document.getElementById('favicon-preview');
            if (favInput) favInput.onchange = () => { const f = favInput.files && favInput.files[0]; if (f) { favPreview.src = URL.createObjectURL(f); favPreview.style.display='inline-block'; } };

            const upFavBtn = document.getElementById('btn-upload-favicon');
            if (upFavBtn) upFavBtn.onclick = async () => {
                const f = favInput.files[0]; if (!f) { this.showNotification('Choose a favicon file', 'error'); return; }
                const fd = new FormData(); fd.append('favicon', f);
                const r = await fetch('/api/admin/site/favicon', { method:'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }, body: fd });
                if (r.ok) { const d = await r.json(); favPreview.src = d.favicon_path || favPreview.src; favPreview.style.display='inline-block'; this.showNotification('Favicon uploaded'); await this.applyPublicSiteSettings(); }
                else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Upload failed','error'); }
            };

            const socialInput = document.getElementById('social-image-file');
            const socialPreview = document.getElementById('social-image-preview');
            if (socialInput) socialInput.onchange = () => { const f = socialInput.files && socialInput.files[0]; if (f) { socialPreview.src = URL.createObjectURL(f); socialPreview.style.display='inline-block'; } };

            const upSocialBtn = document.getElementById('btn-upload-social');
            if (upSocialBtn) upSocialBtn.onclick = async () => {
                const f = socialInput.files[0]; if (!f) { this.showNotification('Choose a social image file', 'error'); return; }
                const fd = new FormData(); fd.append('image', f);
                const r = await fetch('/api/admin/site/social-image', { method:'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }, body: fd });
                if (r.ok) { const d = await r.json(); document.getElementById('social-image').value = d.social_image_url || ''; socialPreview.src = d.social_image_url || socialPreview.src; socialPreview.style.display='inline-block'; this.showNotification('Social image uploaded'); }
                else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Upload failed','error'); }
            };
        }

        const usersSection = document.createElement('section');
        usersSection.className = 'settings-group';
        usersSection.innerHTML = `
          <div class="settings-label">User management</div>
          <input id="user-search" class="settings-input" placeholder="Search users by name or email"/>
          <div id="user-results" style="display:grid;gap:8px"></div>
          <div id="user-pagination" style="display:flex;align-items:center;justify-content:space-between;margin-top:8px">
            <div style="display:flex;gap:8px;align-items:center">
              <button id="user-prev" class="nav-btn" disabled>Prev</button>
              <button id="user-next" class="nav-btn" disabled>Next</button>
            </div>
            <div id="user-page-info" class="meta" style="opacity:.8"></div>
          </div>
        `;

        wrap.appendChild(siteSection);
        wrap.appendChild(usersSection);
        this.gallery.appendChild(wrap);

        if (isAdmin) {
            // Define doSave function with all settings (including storage)
            const doSave = async () => {
                const rawHost = document.getElementById('smtp-host').value.trim();
                const smtpHost = rawHost.replace(/^https?:\/\//i, ''); // host only
                const body = {
                    site_name: document.getElementById('site-name').value,
                    site_url: document.getElementById('site-url').value,
                    seo_title: document.getElementById('seo-title').value,
                    seo_description: document.getElementById('seo-description').value,
                    social_image_url: document.getElementById('social-image').value,
                    storage_provider: document.getElementById('storage-provider').value,
                    s3_endpoint: document.getElementById('s3-endpoint').value,
                    s3_bucket: document.getElementById('s3-bucket').value,
                    s3_access_key: document.getElementById('s3-access').value,
                    s3_secret_key: document.getElementById('s3-secret').value,
                    s3_force_path_style: document.getElementById('s3-path').checked,
                    public_base_url: document.getElementById('public-base').value,
                    smtp_host: smtpHost,
                    smtp_port: parseInt(document.getElementById('smtp-port').value||'0',10),
                    smtp_username: document.getElementById('smtp-username').value,
                    smtp_password: document.getElementById('smtp-password').value,
                    smtp_from_email: document.getElementById('smtp-from').value,
                    smtp_tls: document.getElementById('smtp-tls').checked,
                    require_email_verification: document.getElementById('require-verify')?.checked || false,
                };
                const r = await fetch('/api/admin/site', { method:'PUT', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type':'application/json' }, body: JSON.stringify(body) });
                if (r.ok) { this.showNotification('Saved'); await this.applyPublicSiteSettings(); }
                else { this.showNotification('Save failed','error'); }
            };

            // Wire up all event handlers
            const saveBtnTop = document.getElementById('btn-save-site-top');
            if (saveBtnTop) saveBtnTop.onclick = doSave;
            const saveBtn = document.getElementById('btn-save-site');
            if (saveBtn) saveBtn.onclick = doSave;

            // Wire SMTP test
            const btnTest = document.getElementById('btn-smtp-test');
            if (btnTest) btnTest.onclick = async () => {
                const to = (document.getElementById('smtp-test-to').value||'').trim();
                if(!to){ this.showNotification('Enter recipient','error'); return;}
                const r = await fetch('/api/admin/site/test-smtp', {
                    method:'POST',
                    headers:{ 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type':'application/json' },
                    body: JSON.stringify({ to })
                });
                if (r.status===204) this.showNotification('Test email sent');
                else {
                    const e = await r.json().catch(()=>({}));
                    const msg = e.details ? `${e.error||'Send failed'}: ${e.details}` : (e.error||'Send failed');
                    this.showNotification(msg,'error');
                }
            };

            // Wire storage test
            const btnTestStorage = document.getElementById('btn-test-storage');
            if (btnTestStorage) btnTestStorage.onclick = async () => {
                const r = await fetch('/api/admin/site/test-storage', { method:'POST', headers:{ 'Authorization': `Bearer ${localStorage.getItem('token')}` }});
                const statusEl = document.getElementById('storage-status');
                if (r.ok) {
                    const d = await r.json().catch(()=>({}));
                    if (statusEl) statusEl.textContent = `Current: ${d.provider||'local'} • OK`;
                    this.showNotification('Storage verified');
                } else {
                    const e = await r.json().catch(()=>({}));
                    if (statusEl) statusEl.textContent = `Current: ${document.getElementById('storage-provider').value} • Error`;
                    this.showNotification(e.error||'Storage verification failed','error');
                }
            };

            // Wire export uploads
            const exportBtn = document.getElementById('btn-export-upload');
            if (exportBtn) exportBtn.onclick = () => this.showMigrationModal();

            const favInput = document.getElementById('favicon-file');
            const favPreview = document.getElementById('favicon-preview');
            if (favInput) favInput.onchange = () => { const f = favInput.files && favInput.files[0]; if (f) { favPreview.src = URL.createObjectURL(f); favPreview.style.display='inline-block'; } };

            const upFavBtn = document.getElementById('btn-upload-favicon');
            if (upFavBtn) upFavBtn.onclick = async () => {
                const f = favInput.files[0]; if (!f) { this.showNotification('Choose a favicon file', 'error'); return; }
                const fd = new FormData(); fd.append('favicon', f);
                const r = await fetch('/api/admin/site/favicon', { method:'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }, body: fd });
                if (r.ok) { const d = await r.json(); favPreview.src = d.favicon_path || favPreview.src; favPreview.style.display='inline-block'; this.showNotification('Favicon uploaded'); await this.applyPublicSiteSettings(); }
                else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Upload failed','error'); }
            };

            const socialInput = document.getElementById('social-image-file');
            const socialPreview = document.getElementById('social-image-preview');
            if (socialInput) socialInput.onchange = () => { const f = socialInput.files && socialInput.files[0]; if (f) { socialPreview.src = URL.createObjectURL(f); socialPreview.style.display='inline-block'; } };

            const upSocialBtn = document.getElementById('btn-upload-social');
            if (upSocialBtn) upSocialBtn.onclick = async () => {
                const f = socialInput.files[0]; if (!f) { this.showNotification('Choose a social image file', 'error'); return; }
                const fd = new FormData(); fd.append('image', f);
                const r = await fetch('/api/admin/site/social-image', { method:'POST', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }, body: fd });
                if (r.ok) { const d = await r.json(); document.getElementById('social-image').value = d.social_image_url || ''; socialPreview.src = d.social_image_url || socialPreview.src; socialPreview.style.display='inline-block'; this.showNotification('Social image uploaded'); }
                else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Upload failed','error'); }
            };
        }

        // User search (kept) + actions per result
        const isAdminLocal = isAdmin; // capture for closures
        const searchInput = document.getElementById('user-search');
        const results = document.getElementById('user-results');
        let timer;
        const renderRows = (users=[]) => {
            results.innerHTML = '';
            users.forEach(u => {
                const row = document.createElement('div');
                row.className = 'user-row';
                const left = document.createElement('div'); left.className = 'left'; left.style.minWidth='0';
                left.innerHTML = `<div style="font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">@${u.username}</div><div class="id">${u.id}</div>`;
                const right = document.createElement('div'); right.className='actions';
                const modBtn = document.createElement('button'); modBtn.className='nav-btn'; modBtn.textContent = u.is_moderator ? 'Unmod' : 'Make mod';
                modBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type':'application/json' }, body: JSON.stringify({ is_moderator: !u.is_moderator }) }); if (r.ok) { u.is_moderator = !u.is_moderator; modBtn.textContent = u.is_moderator ? 'Unmod' : 'Make mod'; } };
                right.appendChild(modBtn);
                if (isAdminLocal) {
                    const adminBtn = document.createElement('button'); adminBtn.className='nav-btn'; adminBtn.textContent = u.is_admin ? 'Revoke admin' : 'Make admin';
                    adminBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type':'application/json' }, body: JSON.stringify({ is_admin: !u.is_admin }) }); if (r.ok) { u.is_admin = !u.is_admin; adminBtn.textContent = u.is_admin ? 'Revoke admin' : 'Make admin'; } };
                    right.appendChild(adminBtn);
                    const disableBtn = document.createElement('button'); disableBtn.className='nav-btn'; disableBtn.textContent = u.is_disabled ? 'Enable' : 'Disable';
                    disableBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}`, 'Content-Type':'application/json' }, body: JSON.stringify({ is_disabled: !u.is_disabled }) }); if (r.ok) { u.is_disabled = !u.is_disabled; disableBtn.textContent = u.is_disabled ? 'Enable' : 'Disable'; } };
                    right.appendChild(disableBtn);
                    const verifyBtn = document.createElement('button'); verifyBtn.className='nav-btn'; verifyBtn.textContent='Send verify';
                    verifyBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}/send-verification`, { method:'POST', headers:{ 'Authorization': `Bearer ${localStorage.getItem('token')}` } }); if (r.status===204) this.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Failed','error'); } };
                    right.appendChild(verifyBtn);
                    const delBtn = document.createElement('button'); delBtn.className='nav-btn'; delBtn.style.background='var(--color-danger)'; delBtn.style.color='#fff'; delBtn.textContent='Delete';
                    delBtn.onclick = async () => { const ok = await this.showConfirm('Delete user?'); if (!ok) return; const r = await fetch(`/api/admin/users/${u.id}`, { method:'DELETE', headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }}); if (r.status===204) { row.remove(); } else { this.showNotification('Delete failed','error'); } };
                    right.appendChild(delBtn);
                }
                row.appendChild(left); row.appendChild(right); results.appendChild(row);
            });
        };
        // Pagination state
        const prevBtn = document.getElementById('user-prev');
        const nextBtn = document.getElementById('user-next');
        const pageInfo = document.getElementById('user-page-info');
        let currentPage = 1;
        const pageSize = 50;

        const updatePager = (page, totalPages, total) => {
            currentPage = page;
            prevBtn.disabled = page <= 1;
            nextBtn.disabled = totalPages <= 1 || page >= totalPages;
            pageInfo.textContent = totalPages ? `Page ${page} of ${totalPages} • ${total} users` : '';
        };

        const doSearch = async (q, page = 1) => {
            if (!q) { results.innerHTML = ''; updatePager(1, 0, 0); return; }
            const r = await fetch(`/api/admin/users?q=${encodeURIComponent(q)}&page=${page}&limit=${pageSize}`, { headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }});
            if (r.ok) {
                const d = await r.json();
                renderRows(d.users||[]);
                updatePager(d.page||1, d.total_pages||0, d.total||0);
            }
        };
        searchInput.addEventListener('input', (e) => { clearTimeout(timer); timer = setTimeout(() => doSearch(e.target.value.trim(), 1), 250); });
        prevBtn?.addEventListener('click', () => { const q = searchInput.value.trim(); if (!q) return; doSearch(q, Math.max(1, currentPage - 1)); });
        nextBtn?.addEventListener('click', () => { const q = searchInput.value.trim(); if (!q) return; doSearch(q, currentPage + 1); });
    }

    async renderResetPage() {
        const token = new URLSearchParams(location.search).get('token') || '';
        this.gallery.innerHTML = '';
        if (this.profileTop) this.profileTop.innerHTML = '';
        const wrap = document.createElement('div'); wrap.className='settings-wrap';
        wrap.innerHTML = `
          <section class="settings-group">
            <div class="settings-label">Reset password</div>
            <input type="password" id="rp-new" class="settings-input" placeholder="New password"/>
            <input type="password" id="rp-confirm" class="settings-input" placeholder="Confirm new password"/>
            <div class="settings-actions"><button id="rp-save" class="nav-btn">Save</button></div>
          </section>`;
        this.gallery.appendChild(wrap);
        document.getElementById('rp-save').onclick = async () => {
            const a = document.getElementById('rp-new').value, b = document.getElementById('rp-confirm').value; if (a.length<6 || a!==b) { this.showNotification('Passwords must match and be 6+ chars','error'); return; }
            const r = await fetch('/api/reset-password', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ token, new_password:a }) });
            if (r.status===204) { this.showNotification('Password updated'); history.pushState({}, '', '/'); this.init(); }
            else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Reset failed','error'); }
        };
    }

    async renderVerifyPage() {
        const token = new URLSearchParams(location.search).get('token') || '';
        try { const r = await fetch('/api/verify-email', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ token }) }); if (r.status===204) this.showNotification('Email verified'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Verification failed','error'); } } catch {}
        history.replaceState({}, '', '/'); this.init();
    }

    async openForgotPassword() {
        const overlay = document.createElement('div'); overlay.style.cssText='position:fixed;inset:0;z-index:3050;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
        const panel = document.createElement('div'); panel.style.cssText='max-width:420px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
        panel.innerHTML = `<div class="settings-label">Reset your password</div><input id="fp-email" type="email" class="settings-input" placeholder="Email"/><div class="settings-actions"><button id="fp-send" class="nav-btn">Send reset link</button></div>`;
        overlay.appendChild(panel); document.body.appendChild(overlay);
        overlay.addEventListener('click', (e)=>{ if(e.target===overlay) overlay.remove(); });
        panel.querySelector('#fp-send').onclick = async () => { const email = panel.querySelector('#fp-email').value.trim(); if(!email){ this.showNotification('Enter your email','error'); return; } const r = await fetch('/api/forgot-password',{ method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ email }) }); if (r.status===204){ this.showNotification('Check your email'); overlay.remove(); } else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Unable to send','error'); } };
    }

    async renderImagePage(id) {
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.innerHTML = '';
        this.gallery.classList.add('settings-mode');
        let data = null;
        try {
            const r = await fetch(`/api/images/${encodeURIComponent(id)}`);
            if (!r.ok) throw new Error('not found');
            data = await r.json();
        } catch {
            const wrap = document.createElement('section');
            wrap.className = 'mono-col';
            wrap.style.cssText = 'margin:0 auto 16px;max-width:720px;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--surface-elevated);color:var(--text-primary)';
            wrap.innerHTML = `<div style="font-weight:800;letter-spacing:-0.02em;margin-bottom:6px">Image not found</div><div style="color:var(--text-secondary);font-family:var(--font-mono)">The image may have been removed.</div>`;
            this.gallery.appendChild(wrap);
            return;
        }
        const wrap = document.createElement('section');
        wrap.className = 'mono-col';
        wrap.style.cssText = 'margin:0 auto 16px;max-width:980px;padding:16px;color:var(--text-primary)';
        const title = (data.original_name || 'Untitled');
        const username = data.username || 'unknown';
        const captionHtml = data.caption ? `<div class="image-caption" id="single-caption" style="margin-top:8px;color:var(--text-secondary);position:relative">${this.sanitizeAndRenderMarkdown(data.caption)}</div>` : '';
        wrap.innerHTML = `
          <div style="display:grid;gap:12px">
            <div style="display:flex;align-items:baseline;justify-content:space-between;gap:12px">
              <h1 style="font-size:1.25rem;margin:0;letter-spacing:-0.01em">${title}</h1>
              <a href="/@${encodeURIComponent(username)}" class="link-btn" style="text-decoration:none">@${username}</a>
            </div>
            <div style="display:flex;justify-content:center"><img src="${this.getImageURL(data.filename)}" alt="${title}" style="max-width:100%;max-height:76vh;border-radius:10px;border:1px solid var(--border)"/></div>
            ${captionHtml}
          </div>`;
        this.gallery.appendChild(wrap);

        // Collapsible caption: clamp long captions and toggle on click
        const cap = wrap.querySelector('#single-caption');
        if (cap) {
            const clampPx = 280; // ~14-16 lines depending on line-height
            cap.style.position = 'relative';
            cap.style.transition = 'max-height 240ms var(--ease-smooth)';
            cap.style.cursor = 'default';

            const fade = document.createElement('div');
            fade.style.cssText = 'position:absolute;left:0;right:0;bottom:0;height:48px;background:linear-gradient(180deg, rgba(0,0,0,0), var(--surface));pointer-events:none;display:none';
            cap.appendChild(fade);

            // Toggle row BELOW the caption (collapsed state)
            const toggleRow = document.createElement('div');
            toggleRow.id = 'cap-toggle-row';
            toggleRow.style.cssText = 'display:none;text-align:center;margin-top:6px;';
            const toggleBtn = document.createElement('button');
            toggleBtn.type = 'button'; toggleBtn.className = 'link-btn';
            toggleBtn.setAttribute('aria-label', 'Expand caption');
            toggleBtn.setAttribute('aria-controls', 'single-caption');
            toggleBtn.innerHTML = 'Show more <svg id="cap-toggle-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-left:4px;transition:transform 200ms var(--ease-smooth)"><polyline points="6 9 12 15 18 9"/></svg>';
            toggleRow.appendChild(toggleBtn);
            cap.parentNode.insertBefore(toggleRow, cap.nextSibling);

            // Toggle INSIDE the caption at the bottom (expanded state)
            const toggleInside = document.createElement('div');
            toggleInside.id = 'cap-toggle-inside';
            toggleInside.style.cssText = 'display:none;margin-top:8px;text-align:center;';
            const toggleInsideBtn = document.createElement('button');
            toggleInsideBtn.type = 'button'; toggleInsideBtn.className = 'link-btn';
            toggleInsideBtn.setAttribute('aria-label', 'Collapse caption');
            toggleInsideBtn.setAttribute('aria-controls', 'single-caption');
            toggleInsideBtn.innerHTML = 'Show less <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-left:4px;transform:rotate(180deg)"><polyline points="6 9 12 15 18 9"/></svg>';
            toggleInside.appendChild(toggleInsideBtn);
            cap.appendChild(toggleInside);

            let collapsed = true;
            const apply = () => {
                const overflows = cap.scrollHeight > clampPx + 1; // +1 buffer
                if (collapsed) {
                    cap.style.maxHeight = clampPx + 'px';
                    cap.style.overflow = 'hidden';
                    cap.style.paddingBottom = '0px';
                    fade.style.display = overflows ? 'block' : 'none';
                    toggleRow.style.display = overflows ? 'block' : 'none';
                    toggleBtn.setAttribute('aria-expanded', 'false');
                    toggleInside.style.display = 'none';
                } else {
                    // Ensure inside toggle is visible before measuring full height
                    toggleInside.style.display = 'block';
                    toggleRow.style.display = 'none';
                    cap.style.overflow = 'visible';
                    cap.style.paddingBottom = '16px';
                    fade.style.display = 'none';
                    // Expand to full content height smoothly
                    const full = cap.scrollHeight;
                    cap.style.maxHeight = full + 'px';
                    toggleBtn.setAttribute('aria-expanded', 'true');
                }
            };

            // Initial layout apply after rendering to get accurate scrollHeight
            requestAnimationFrame(apply);

            const doToggle = () => { collapsed = !collapsed; apply(); };

            // Only toggle via explicit controls
            toggleBtn.addEventListener('click', (e) => { e.stopPropagation(); doToggle(); });
            toggleInsideBtn.addEventListener('click', (e) => { e.stopPropagation(); doToggle(); });
        }

        // Back/forward support
        window.onpopstate = () => {
            if (location.pathname.startsWith('/i/')) {
                const id2 = location.pathname.split('/')[2];
                this.renderImagePage(id2);
            } else if (location.pathname.startsWith('/@')) {
                const u = decodeURIComponent(location.pathname.slice(2));
                this.renderProfilePage(u);
            } else if (location.pathname === '/settings') {
                this.renderSettingsPage();
            } else if (location.pathname === '/admin') {
                this.renderAdminPage();
            } else {
                this.gallery.classList.remove('settings-mode');
                this.gallery.innerHTML = ''; if (this.profileTop) this.profileTop.innerHTML=''; this.page=1; this.hasMore=true; this.loadImages();
            }
        };
    }

    showMigrationModal() {
        const overlay = document.createElement('div');
        overlay.style.cssText = 'position:fixed;inset:0;z-index:3000;background:rgba(0,0,0,0.8);backdrop-filter:blur(12px);display:flex;align-items:center;justify-content:center;padding:24px;';
        
        const modal = document.createElement('div');
        modal.style.cssText = 'max-width:560px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:16px;overflow:hidden;box-shadow:var(--shadow-3);';
        
        const header = document.createElement('div');
        header.style.cssText = 'padding:24px 24px 16px;border-bottom:1px solid var(--border);';
        header.innerHTML = `
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
                <h2 style="margin:0;font-size:1.25rem;font-weight:var(--weight-semibold);color:var(--text-primary)">Migrate to Remote Storage</h2>
                <button id="close-migration" style="background:none;border:none;color:var(--text-secondary);font-size:1.5rem;cursor:pointer;padding:4px;border-radius:4px" title="Close">&times;</button>
            </div>
            <p style="margin:0;color:var(--text-secondary);font-size:0.9rem;line-height:1.4">Move all local uploads to your configured remote storage and update image URLs in the database.</p>
        `;

        const content = document.createElement('div');
        content.style.cssText = 'padding:16px 24px 24px;';
        content.innerHTML = `
            <div id="migration-status" style="display:none;margin-bottom:16px;padding:12px;background:var(--surface);border:1px solid var(--border);border-radius:8px;">
                <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">
                    <div id="migration-spinner" style="width:16px;height:16px;border:2px solid var(--border);border-top:2px solid var(--accent);border-radius:50%;animation:spin 1s linear infinite"></div>
                    <span id="migration-phase" style="font-weight:var(--weight-medium);color:var(--text-primary)">Preparing migration...</span>
                </div>
                <div id="migration-progress" style="background:var(--surface-elevated);border-radius:4px;height:6px;overflow:hidden;margin-bottom:8px">
                    <div id="migration-bar" style="height:100%;background:var(--accent);width:0%;transition:width 0.3s ease"></div>
                </div>
                <div id="migration-details" style="font-size:0.8rem;color:var(--text-secondary)"></div>
            </div>

            <div id="migration-options" style="display:grid;gap:12px">
                <label style="display:flex;align-items:center;gap:8px;padding:12px;border:1px solid var(--border);border-radius:8px;cursor:pointer;transition:all 0.2s ease" onmouseover="this.style.borderColor='var(--accent)'" onmouseout="this.style.borderColor='var(--border)'">
                    <input type="checkbox" id="cleanup-local" style="accent-color:var(--accent)"/>
                    <div>
                        <div style="font-weight:var(--weight-medium);color:var(--text-primary);margin-bottom:2px">Delete local files after successful upload</div>
                        <div style="font-size:0.8rem;color:var(--text-secondary)">Recommended: Saves disk space by removing local copies once they're safely stored remotely</div>
                    </div>
                </label>
            </div>

            <div id="migration-results" style="display:none;margin-top:16px;padding:12px;border-radius:8px;"></div>

            <div id="migration-actions" style="display:flex;gap:8px;justify-content:flex-end;margin-top:20px">
                <button id="cancel-migration" class="nav-btn" style="background:var(--surface-elevated);color:var(--text-primary);border:1px solid var(--border);padding:8px 16px">Cancel</button>
                <button id="start-migration" class="nav-btn" style="background:var(--color-warn, #f2c94c);color:var(--color-bg, #0a0a0a);border:none;font-weight:var(--weight-medium, 500);padding:8px 16px">Start Migration</button>
            </div>
        `;

        modal.appendChild(header);
        modal.appendChild(content);
        overlay.appendChild(modal);
        document.body.appendChild(overlay);

        // Add spinner animation
        const style = document.createElement('style');
        style.textContent = '@keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }';
        document.head.appendChild(style);

        // Wire up event handlers
        const closeMigration = () => {
            if (overlay.parentNode) {
                overlay.parentNode.removeChild(overlay);
            }
            if (style.parentNode) {
                style.parentNode.removeChild(style);
            }
        };

        document.getElementById('close-migration').onclick = closeMigration;
        document.getElementById('cancel-migration').onclick = closeMigration;

        document.getElementById('start-migration').onclick = async () => {
            const cleanupLocal = document.getElementById('cleanup-local').checked;
            await this.performMigration(cleanupLocal, closeMigration);
        };

        // Close on overlay click
        overlay.onclick = (e) => {
            if (e.target === overlay) closeMigration();
        };

        // Close on Escape
        const keyHandler = (e) => {
            if (e.key === 'Escape') {
                closeMigration();
                document.removeEventListener('keydown', keyHandler);
            }
        };
        document.addEventListener('keydown', keyHandler);
    }

    async performMigration(cleanupLocal, closeModalFunction) {
        const statusEl = document.getElementById('migration-status');
        const optionsEl = document.getElementById('migration-options');
        const actionsEl = document.getElementById('migration-actions');
        const phaseEl = document.getElementById('migration-phase');
        const progressBar = document.getElementById('migration-bar');
        const detailsEl = document.getElementById('migration-details');
        const resultsEl = document.getElementById('migration-results');

        // Show progress UI
        statusEl.style.display = 'block';
        optionsEl.style.display = 'none';
        actionsEl.style.display = 'none';

        try {
            phaseEl.textContent = 'Starting migration...';
            progressBar.style.width = '20%';
            detailsEl.textContent = 'Preparing to transfer files...';

            // Simulate progress updates
            setTimeout(() => {
                if (progressBar.style.width === '20%') {
                    progressBar.style.width = '40%';
                    detailsEl.textContent = 'Uploading files to remote storage...';
                }
            }, 500);

            const response = await fetch('/api/admin/site/export-uploads', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${localStorage.getItem('token')}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ cleanup_local: cleanupLocal })
            });

            progressBar.style.width = '80%';
            detailsEl.textContent = 'Processing response...';

            const result = await response.json();

            progressBar.style.width = '100%';

            if (response.ok && result.success) {
                // Stop spinner and update phase
                const spinner = document.getElementById('migration-spinner');
                if (spinner) spinner.style.display = 'none';
                
                phaseEl.textContent = 'Migration completed successfully!';
                detailsEl.textContent = 'All files have been transferred and database updated.';
                
                resultsEl.style.display = 'block';
                resultsEl.style.background = 'var(--color-ok-bg, #0f2e1f)';
                resultsEl.style.border = '1px solid var(--color-ok, #4ade80)';
                resultsEl.style.color = 'var(--color-ok, #4ade80)';
                resultsEl.innerHTML = `
                    <div style="font-weight:var(--weight-medium, 500);margin-bottom:8px">✅ Migration Summary</div>
                    <div style="font-size:0.9rem;line-height:1.4">
                        • <strong>${result.uploaded_files || 0}</strong> files uploaded to remote storage<br>
                        • <strong>${result.updated_records || 0}</strong> database records updated<br>
                        ${result.cleaned_files > 0 ? `• <strong>${result.cleaned_files}</strong> local files cleaned up<br>` : ''}
                        ${result.total_files > 0 ? `• Total files processed: <strong>${result.total_files}</strong>` : ''}
                    </div>
                `;

                this.showNotification('Migration completed successfully!', 'success');
            } else {
                throw new Error(result.error || 'Migration failed');
            }
        } catch (error) {
            // Stop spinner on error
            const spinner = document.getElementById('migration-spinner');
            if (spinner) spinner.style.display = 'none';
            
            phaseEl.textContent = 'Migration failed';
            detailsEl.textContent = error.message;
            progressBar.style.background = 'var(--color-danger, #ef4444)';

            resultsEl.style.display = 'block';
            resultsEl.style.background = 'var(--color-danger-bg, #2e1a1a)';
            resultsEl.style.border = '1px solid var(--color-danger, #ef4444)';
            resultsEl.style.color = 'var(--color-danger, #ef4444)';
            resultsEl.innerHTML = `
                <div style="font-weight:var(--weight-medium, 500);margin-bottom:8px">❌ Migration Failed</div>
                <div style="font-size:0.9rem">${error.message}</div>
            `;

            this.showNotification('Migration failed: ' + error.message, 'error');
        }

        // Show close button
        actionsEl.style.display = 'flex';
        actionsEl.innerHTML = '<button id="close-migration-final" class="nav-btn" style="background:var(--surface-elevated);color:var(--text-primary);border:1px solid var(--border);margin-left:auto;padding:8px 16px;font-weight:var(--weight-medium, 500)">Close</button>';
        
        document.getElementById('close-migration-final').onclick = closeModalFunction;
    }
}

// MagneticScroll: gentle, oozing drift-to-focus behavior
class MagneticScroll {
    constructor(options = {}) {
        // Configuration for drift-based oozing feel
        this.config = {
            settleDelay: 150,
            driftCheckInterval: 16,
            maxDriftSpeed: 0.8,
            minDriftSpeed: 0.05,
            attractionStrength: 0.015,
            damping: 0.92,
            effectiveRange: 200,
            minCardIndex: 2,
            captionMaxHeight: 0.4,
            velocityMemory: 5,
            highVelocityThreshold: 8,
            ...options
        };

        // State for drift-based behavior
        this.state = {
            enabled: false,
            isDrifting: false,
            currentVelocity: 0,
            targetPosition: null,
            lastScrollY: 0,
            lastScrollTime: 0,
            settleTimer: null,
            driftAnimationId: null
        };

        // Velocity tracking
        this.velocityBuffer = [];
        
        // Touch tracking
        this.touch = {
            active: false,
            startY: 0,
            startTime: 0,
            velocities: []
        };

        // Bind methods
        this.handleScroll = this.handleScroll.bind(this);
        this.handleTouchStart = this.handleTouchStart.bind(this);
        this.handleTouchMove = this.handleTouchMove.bind(this);
        this.handleTouchEnd = this.handleTouchEnd.bind(this);
        this.handleWheel = this.handleWheel.bind(this);
        this.updateDrift = this.updateDrift.bind(this);

        this.init();
    }

    init() {
        this.updateEnabledState();
        window.addEventListener('scroll', this.handleScroll, { passive: true });
        window.addEventListener('touchstart', this.handleTouchStart, { passive: true });
        window.addEventListener('touchmove', this.handleTouchMove, { passive: true });
        window.addEventListener('touchend', this.handleTouchEnd, { passive: true });
        window.addEventListener('wheel', this.handleWheel, { passive: true });
        window.addEventListener('resize', () => this.updateEnabledState());
        window.addEventListener('orientationchange', () => this.updateEnabledState());
        // Inject optional CSS once
        if (!document.getElementById('magnetic-scroll-style')) {
            const style = document.createElement('style');
            style.id = 'magnetic-scroll-style';
            style.textContent = `
                .image-card { transition: transform 0.3s cubic-bezier(0.23, 1, 0.32, 1); }
                .image-card.in-focus { transform: scale(1.01); }
                html { scroll-behavior: auto !important; }
            `;
            document.head.appendChild(style);
        }
    }

    destroy() {
        this.stopDrift();
        clearTimeout(this.state.settleTimer);
        window.removeEventListener('scroll', this.handleScroll);
        window.removeEventListener('touchstart', this.handleTouchStart);
        window.removeEventListener('touchmove', this.handleTouchMove);
        window.removeEventListener('touchend', this.handleTouchEnd);
        window.removeEventListener('wheel', this.handleWheel);
    }

    updateEnabledState() {
        const isMobileWidth = window.matchMedia('(max-width: 768px)').matches;
        const hasTouch = ('ontouchstart' in window) || (navigator.maxTouchPoints > 0) || window.matchMedia('(pointer: coarse)').matches;
        const hasModalOpen = document.body.style.overflow === 'hidden';
        const isSpecialPage = /^\/(settings|admin|reset|verify)/.test(location.pathname);
        this.state.enabled = isMobileWidth && hasTouch && !hasModalOpen && !isSpecialPage;
        
        // Debug logging (can be removed in production)
        // console.log('MagneticScroll.updateEnabledState:', { enabled: this.state.enabled });
        
        if (!this.state.enabled) this.stopDrift();
    }

    handleScroll() {
        if (!this.state.enabled) return;
        // Ignore scroll events generated by our own drift motion
        if (this.state.isDrifting) return;
        const now = performance.now();
        const scrollY = window.scrollY;
        const dt = now - this.state.lastScrollTime;
        if (dt > 0 && dt < 100) this.trackVelocity((scrollY - this.state.lastScrollY) / dt);
        this.state.lastScrollY = scrollY;
        this.state.lastScrollTime = now;
        this.scheduleSettle();
    }

    handleTouchStart(e) {
        if (!this.state.enabled) return;
        const t = e.touches[0]; if (!t) return;
        this.touch = { active: true, startY: t.clientY, startTime: performance.now(), velocities: [] };
        this.stopDrift();
    }

    handleTouchMove(e) {
        if (!this.state.enabled || !this.touch.active) return;
        const t = e.touches[0]; if (!t) return;
        const now = performance.now(); const dt = now - this.touch.startTime;
        if (dt > 16) {
            const dy = t.clientY - this.touch.startY; const v = dy / dt;
            this.touch.velocities.push(v); if (this.touch.velocities.length > 5) this.touch.velocities.shift();
            this.touch.startY = t.clientY; this.touch.startTime = now;
        }
    }

    handleTouchEnd() {
        if (!this.state.enabled || !this.touch.active) return;
        this.touch.active = false;
        if (this.touch.velocities.length > 0) {
            const avg = this.touch.velocities.reduce((a,b)=>a+b,0) / this.touch.velocities.length;
            // Convert px/frame to px/ms (assuming 60fps = 16.67ms per frame)
            const threshold = this.config.highVelocityThreshold / 16.67;
            // console.log('MagneticScroll.handleTouchEnd velocity check:', { avg, threshold, velocities: this.touch.velocities });
            if (Math.abs(avg) > threshold) this.scheduleSettle(400);
            else this.scheduleSettle(this.config.settleDelay);
        } else {
            this.scheduleSettle(this.config.settleDelay);
        }
    }

    handleWheel() {
        if (!this.state.enabled) return;
        this.stopDrift();
        this.scheduleSettle(200);
    }

    trackVelocity(position) {
        const now = performance.now();
        const dt = now - this.velocityTracker.lastTime;
        if (dt > 0) {
            this.velocityTracker.positions.push({ y: position, time: now });
            if (this.velocityTracker.positions.length > this.velocityTracker.maxSize) this.velocityTracker.positions.shift();
        }
        this.velocityTracker.lastTime = now;
    }

    trackVelocity(v) {
        this.velocityBuffer.push(v);
        if (this.velocityBuffer.length > this.config.velocityMemory) this.velocityBuffer.shift();
    }

    getAverageVelocity() {
        if (!this.velocityBuffer.length) return 0;
        return this.velocityBuffer.reduce((a, b) => a + b, 0) / this.velocityBuffer.length;
    }

    scheduleSettle(delay = null) {
        clearTimeout(this.state.settleTimer);
        const d = delay ?? this.config.settleDelay;
        // console.log('MagneticScroll.scheduleSettle:', { delay: d, enabled: this.state.enabled });
        this.state.settleTimer = setTimeout(() => this.beginDrift(), d);
    }

    hasExpandedCaptionInView() {
        const cards = Array.from(document.querySelectorAll('.image-card'));
        const viewportTop = window.scrollY;
        const viewportHeight = window.innerHeight;
        
        return cards.some(card => {
            const caption = card.querySelector('.image-caption');
            if (!caption?.classList.contains('expanded')) return false;
            
            const rect = card.getBoundingClientRect();
            const cardTop = rect.top + viewportTop;
            const cardBottom = cardTop + rect.height;
            
            // Check if card overlaps with viewport significantly
            const visibleTop = Math.max(cardTop, viewportTop);
            const visibleBottom = Math.min(cardBottom, viewportTop + viewportHeight);
            const visibleHeight = Math.max(0, visibleBottom - visibleTop);
            const visibilityRatio = visibleHeight / viewportHeight;
            
            return visibilityRatio > 0.3; // Card takes up significant viewport space
        });
    }

    beginDrift() {
        // console.log('MagneticScroll.beginDrift called:', { enabled: this.state.enabled, isDrifting: this.state.isDrifting });
        if (!this.state.enabled || this.state.isDrifting) return;
        
        // Don't drift if user is reading an expanded caption
        if (this.hasExpandedCaptionInView()) {
            // console.log('MagneticScroll.beginDrift blocked: expanded caption in view');
            return;
        }
        
        const point = this.findAttractionPoint();
        // console.log('MagneticScroll.beginDrift attraction point:', point);
        if (!point) return;
        const currentY = window.scrollY; const distance = Math.abs(point.position - currentY);
        // console.log('MagneticScroll.beginDrift distance check:', { currentY, targetY: point.position, distance });
        if (distance < 3) return; // Increased threshold to prevent micro-adjustments
        this.state.targetPosition = point.position; this.state.currentVelocity = 0; this.state.isDrifting = true;
        // console.log('MagneticScroll.beginDrift starting drift to:', point.position);
        this.highlightCard(point.card);
        this.updateDrift();
    }

    updateDrift() {
        if (!this.state.isDrifting || !this.state.enabled) { this.stopDrift(); return; }
        const currentY = window.scrollY; 
        const targetY = this.state.targetPosition; 
        let distance = targetY - currentY;
        
        // Smooth stop when very close
        if (Math.abs(distance) < 1) { 
            this.smoothScrollTo(targetY, 150);
            this.stopDrift(); 
            return; 
        }
        
        // Dynamic attraction force - stronger when closer, with smooth falloff
        const normalizedDistance = Math.min(1, Math.abs(distance) / this.config.effectiveRange);
        const attractionCurve = Math.pow(1 - normalizedDistance, 1.5); // Smooth curve
        const baseForce = this.config.attractionStrength * attractionCurve;
        
        // Prevent overshoot by reducing force when velocity is in same direction as distance
        const velocityDirection = Math.sign(this.state.currentVelocity);
        const distanceDirection = Math.sign(distance);
        const overshootDamping = (velocityDirection === distanceDirection && Math.abs(this.state.currentVelocity) > 0.3) ? 0.5 : 1;
        
        const force = distanceDirection * baseForce * overshootDamping * Math.min(Math.abs(distance), 30);
        
        // Apply force and damping
        this.state.currentVelocity += force;
        this.state.currentVelocity *= this.config.damping;
        
        // Clamp velocity
        this.state.currentVelocity = Math.max(-this.config.maxDriftSpeed, Math.min(this.config.maxDriftSpeed, this.state.currentVelocity));
        
        // Smooth stop when velocity gets very low
        if (Math.abs(this.state.currentVelocity) < this.config.minDriftSpeed) { 
            this.smoothScrollTo(targetY, 200);
            this.stopDrift(); 
            return; 
        }
        
        // Apply movement
        const newY = currentY + this.state.currentVelocity;
        const maxScroll = document.documentElement.scrollHeight - window.innerHeight; 
        const clampedY = Math.max(0, Math.min(maxScroll, newY));
        window.scrollTo(0, clampedY);
        
        this.state.driftAnimationId = requestAnimationFrame(() => this.updateDrift());
    }

    stopDrift() {
        if (this.state.driftAnimationId) { cancelAnimationFrame(this.state.driftAnimationId); this.state.driftAnimationId = null; }
        this.state.isDrifting = false; this.state.currentVelocity = 0; this.state.targetPosition = null;
    }

    smoothScrollTo(targetY, duration = 300) {
        const startY = window.scrollY;
        const distance = targetY - startY;
        if (Math.abs(distance) < 1) return;
        
        const startTime = performance.now();
        const easeOutCubic = (t) => 1 - Math.pow(1 - t, 3);
        
        const animate = () => {
            const elapsed = performance.now() - startTime;
            const progress = Math.min(1, elapsed / duration);
            const easedProgress = easeOutCubic(progress);
            const currentY = startY + (distance * easedProgress);
            
            window.scrollTo(0, Math.round(currentY));
            
            if (progress < 1) {
                requestAnimationFrame(animate);
            }
        };
        
        requestAnimationFrame(animate);
    }

    findAttractionPoint() {
        const scan = (ignoreMinIndex = false) => {
            const cards = Array.from(document.querySelectorAll('.image-card'));
            if (cards.length === 0) return { bestInRange: null, bestOverall: null };
            const viewportTop = window.scrollY;
            const viewportHeight = window.innerHeight;
            const maxScroll = Math.max(0, document.documentElement.scrollHeight - viewportHeight);

            let bestInRange = null;
            let bestInRangeScore = Infinity;
            let bestOverall = null;
            let bestOverallDist = Infinity;

            cards.forEach((card, index) => {
                if (!ignoreMinIndex && index < this.config.minCardIndex) return;
                const caption = card.querySelector('.image-caption');
                if (caption?.classList.contains('expanded')) {
                    const ch = caption.getBoundingClientRect().height;
                    if (ch / viewportHeight > this.config.captionMaxHeight) return;
                }
                
                const img = card.querySelector('img');
                const cardRect = card.getBoundingClientRect();
                const imgRect = img ? img.getBoundingClientRect() : cardRect;
                
                // Use image center for targeting, but ensure it's visible
                const imgCenter = imgRect.top + viewportTop + (imgRect.height / 2);
                const cardTop = cardRect.top + viewportTop;
                const cardBottom = cardTop + cardRect.height;
                
                // Calculate ideal scroll position to center the image nicely
                const navHeight = document.getElementById('nav')?.offsetHeight || 0;
                const offset = (viewportHeight / 2) - navHeight * 0.25;
                let idealScrollY = imgCenter - offset;
                idealScrollY = Math.max(0, Math.min(maxScroll, idealScrollY));
                
                const distance = Math.abs(idealScrollY - viewportTop);
                
                // Track overall nearest
                if (distance < bestOverallDist) { 
                    bestOverallDist = distance; 
                    bestOverall = { card, position: idealScrollY }; 
                }

                // Only consider cards that are reasonably close and visible
                if (distance < this.config.effectiveRange * 1.5) {
                    const visibleTop = Math.max(cardRect.top, 0);
                    const visibleBottom = Math.min(cardRect.bottom, viewportHeight);
                    const visibleHeight = Math.max(0, visibleBottom - visibleTop);
                    const visibility = cardRect.height > 0 ? visibleHeight / cardRect.height : 0;
                    
                    // Prefer cards that are partially visible and close to ideal position
                    const visibilityBonus = visibility > 0.1 ? (1 - visibility * 0.5) : 1.5; // Slight penalty for fully visible
                    const score = distance * visibilityBonus;
                    
                    if (score < bestInRangeScore) { 
                        bestInRangeScore = score; 
                        bestInRange = { card, position: idealScrollY }; 
                    }
                }
            });
            return { bestInRange, bestOverall };
        };

        // First pass honors minCardIndex
        const pass1 = scan(false);
        if (pass1.bestInRange || pass1.bestOverall) return pass1.bestInRange || pass1.bestOverall;
        // Fallback pass ignores minCardIndex so top-of-feed still works
        const pass2 = scan(true);
        return pass2.bestInRange || pass2.bestOverall;
    }

    highlightCard(card) {
        document.querySelectorAll('.image-card.in-focus, .image-card.focused').forEach(el => { el.classList.remove('in-focus'); el.classList.remove('focused'); });
        if (card) { card.classList.add('in-focus'); card.classList.add('focused'); }
    }

    animateScrollTo(targetY) {
        this.cancelAnimation();
        const startY = window.scrollY;
        const distance = Math.abs(targetY - startY);
        if (distance < 3) { window.scrollTo(0, targetY); return; }
        const duration = Math.min(this.config.animationDuration.max, Math.max(this.config.animationDuration.min, this.config.animationDuration.min + (distance * this.config.animationDuration.perPixel)));
        if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) { window.scrollTo(0, targetY); return; }
        const startTime = performance.now();
        this.state.isAnimating = true;
        const easeOutQuint = (t) => 1 - Math.pow(1 - t, 5);
        const animate = () => {
            if (!this.state.isAnimating) return;
            const now = performance.now();
            const elapsed = now - startTime;
            const progress = Math.min(1, elapsed / duration);
            const eased = easeOutQuint(progress);
            const currentY = startY + (targetY - startY) * eased;
            window.scrollTo(0, Math.round(currentY));
            if (progress < 1) this.state.animationId = requestAnimationFrame(animate);
            else { this.state.isAnimating = false; this.state.animationId = null; }
        };
        this.state.animationId = requestAnimationFrame(animate);
    }

    cancelAnimation() {
        if (this.state.animationId) { cancelAnimationFrame(this.state.animationId); this.state.animationId = null; }
        this.state.isAnimating = false;
    }

    highlightCard(card) {
        document.querySelectorAll('.image-card.magnetic-focused, .image-card.focused').forEach(el => {
            el.classList.remove('magnetic-focused');
            el.classList.remove('focused');
        });
        if (card) {
            card.classList.add('magnetic-focused');
            card.classList.add('focused');
        }
    }
}

// Export for CommonJS if needed
if (typeof module !== 'undefined' && module.exports) {
    module.exports = MagneticScroll;
}

document.addEventListener('DOMContentLoaded', () => { window.app = new TroughApp(); });