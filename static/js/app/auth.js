import { fetchWithCSRF } from './services/api.js';
import { escapeHTML, sanitizeAndRenderMarkdown } from './utils.js';

export default class Auth {
    constructor(app) {
        this.app = app;
        this.currentUser = null;
        this._pendingInvite = '';

        this.authModal = document.getElementById('auth-modal');
        this.authBtn = document.getElementById('auth-btn');

        const cachedUser = localStorage.getItem('user');
        if (cachedUser) {
            try {
                this.currentUser = JSON.parse(cachedUser);
                this.app.currentUser = this.currentUser;
            } catch {}
        }
    }

    async checkAuth() {
        const token = localStorage.getItem('token');
        // First try cookie-based session
        try {
            const resp = await fetchWithCSRF('/api/me', { credentials: 'include' });
            if (resp.ok) {
                const data = await resp.json();
                this.currentUser = data.user;
                this.app.currentUser = data.user;
                localStorage.setItem('user', JSON.stringify(data.user));
                this.updateAuthButton();
                return;
            }
        } catch {}
        // Fallback: try bearer token (useful on mobile HTTP where cookies may be blocked)
        if (token) {
            try {
                const resp2 = await fetchWithCSRF('/api/me', { credentials: 'include', headers: { 'Authorization': `Bearer ${token}` } });
                if (resp2.ok) {
                    const data2 = await resp2.json();
                    this.currentUser = data2.user;
                    this.app.currentUser = data2.user;
                    localStorage.setItem('user', JSON.stringify(data2.user));
                    this.updateAuthButton();
                    return;
                }
            } catch {}
        }
        // If both fail, ensure local logged-out state without pinging server logout
        try { localStorage.removeItem('user'); } catch {}
        this.currentUser = null;
        this.app.currentUser = null;
        this.updateAuthButton();
    }

    updateAuthButton() {
        if (this.currentUser) {
            this.authBtn.textContent = `@${this.currentUser.username}`;
            this.authBtn.title = `@${this.currentUser.username}`;
            this.authBtn.style.fontFamily = 'var(--font-mono)';
            // Refresh my collected set after login
            this.app.seedMyCollectedSet().catch(()=>{});
        } else {
            this.authBtn.textContent = 'ENTER';
            this.authBtn.style.fontFamily = '';
            // Clear collected cache when logged out
            this.app._myCollectedSet = new Set();
        }
    }

    async signOut() {
        // Clear server-side cookie session; keepalive ensures it completes during navigation
        try { await fetchWithCSRF('/api/logout', { method: 'POST', credentials: 'include', keepalive: true }); } catch {}
        try { localStorage.removeItem('token'); localStorage.removeItem('user'); } catch {}
        this.currentUser = null;
        this.app.currentUser = null;
        this.updateAuthButton();
        this.app._myCollectedSet = new Set();
    }

    setupAuthModal() {
        const tabs = document.querySelectorAll('.auth-tab');
        const form = document.getElementById('auth-form');
        const loginForm = document.getElementById('login-form');
        const registerForm = document.getElementById('register-form');
        const submitBtn = document.getElementById('auth-submit');
        // Wire close actions (no inline handlers)
        const ab = document.querySelector('#auth-modal .auth-backdrop');
        const ac = document.querySelector('#auth-modal .auth-close');
        if (ab) ab.addEventListener('click', () => this.closeAuthModal());
        if (ac) ac.addEventListener('click', () => this.closeAuthModal());
        const inviteParam = this._pendingInvite || '';
        const setSubmit = (text, disabled) => { submitBtn.textContent = text; submitBtn.disabled = !!disabled; };
        // Hide register tab if public registration is disabled (unless invite present)
        const registerTab = Array.from(tabs).find(t => t.dataset.tab === 'register');
        if (registerTab && window.__PUBLIC_REG_ENABLED__ === false && !this._pendingInvite) {
            registerTab.style.display = 'none';
            // Ensure login is active
            const loginTab = Array.from(tabs).find(t => t.dataset.tab === 'login');
            if (loginTab) { tabs.forEach(t => t.classList.remove('active')); loginTab.classList.add('active'); }
            if (loginForm && registerForm) { loginForm.style.display = 'block'; registerForm.style.display = 'none'; setSubmit('Sign In', false); }
        }

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
            eye.onclick = () => { const isPass = input.type === 'password'; input.type = isPass ? 'text' : 'password'; /* keep same icon; avoid monkey emoji */ };
            wrap.appendChild(eye);
        };

        // Build eyes for all password fields and remove old text toggles
        ['login-password','register-password','register-password-confirm'].forEach(id => ensureEyeToggle(id));
        document.querySelectorAll('.password-toggle').forEach(el => el.remove());

        const strengthEl = document.getElementById('password-strength');
        const scorePassword = (pwd) => {
            if (!pwd) return 0;
            let score = 0;
            let hasUpper, hasLower, hasNumber, hasSpecial = false;

            for (const char of pwd) {
                if (char >= 'A' && char <= 'Z') hasUpper = true;
                else if (char >= 'a' && char <= 'z') hasLower = true;
                else if (char >= '0' && char <= '9') hasNumber = true;
                else hasSpecial = true;
            }

            let categories = 0;
            if (hasUpper) categories++;
            if (hasLower) categories++;
            if (hasNumber) categories++;
            if (hasSpecial) categories++;

            // Base score on length
            if (pwd.length >= 8) {
                score = 1; // At least 8 chars
            }
            if (pwd.length >= 12) {
                score = 2; // At least 12 chars
            }
            if (pwd.length >= 16) {
                score = 3; // At least 16 chars
            }

            // Add bonus for character categories
            if (categories >= 3) {
                score++;
            }
            if (categories >= 4) {
                score++;
            }

            // Cap score at 4
            if (score > 4) {
                score = 4;
            }

            return score;
        };
        const renderStrength = (pwd) => {
            const score = scorePassword(pwd); // 0..4
            if (strengthEl) {
                const segs = strengthEl.querySelectorAll('.pw-seg .fill');
                segs.forEach((seg, i) => {
                    const active = score >= (i + 1);
                    seg.style.width = active ? '100%' : '0%';
                    if (i === 3) seg.classList.toggle('shimmer', score === 4);
                });
            }
            return score;
        };

        tabs.forEach(tab => {
            tab.addEventListener('click', () => {
                const tabType = tab.dataset.tab;
                if (tabType === 'register' && window.__PUBLIC_REG_ENABLED__ === false && !this._pendingInvite) {
                    this.showAuthError('Registration is currently disabled');
                    return;
                }
                tabs.forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                if (tabType === 'login') { loginForm.style.display = 'block'; registerForm.style.display = 'none'; setSubmit('Sign In', false); }
                else {
                    loginForm.style.display = 'none';
                    registerForm.style.display = 'block';
                    setSubmit('Create Account', false);
                    // Fetch and display password requirements
                    this.fetchPasswordRequirements();
                }
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
            if (!isLogin && window.__PUBLIC_REG_ENABLED__ === false && !this._pendingInvite) {
                this.showAuthError('Registration is currently disabled');
                return;
            }
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
        this.app.showLoader(); this.hideAuthError();
        try {
            const response = await fetchWithCSRF('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify({ email, password }) });
            const data = await response.json().catch(() => ({}));
            if (response.ok) {
                // Prefer cookie-based session; still cache user locally for UI
                try {
                    if (data && data.token) localStorage.setItem('token', data.token);
                } catch {}
                localStorage.setItem('user', JSON.stringify(data.user));
                this.currentUser = data.user;
                this.app.currentUser = data.user;
                this.closeAuthModal(); this.updateAuthButton(); this.app.showNotification('Welcome back!', 'success');
            } else if (response.status === 403 && data.error && /verify/i.test(data.error)) {
                this.showAuthError('Email not verified. Please check your inbox.');
            } else {
                this.showAuthError(data.error || 'Login failed');
            }
        } catch (error) {
            this.showAuthError('Connection error. Please try again.');
        }
        this.app.hideLoader();
    }

    async handleRegister() {
        const username = document.getElementById('register-username').value.trim();
        const email = document.getElementById('register-email').value.trim();
        const password = document.getElementById('register-password').value;
        const confirm = document.getElementById('register-password-confirm').value;
        const invite = this._pendingInvite || new URL(location.href).searchParams.get('invite') || '';

        // Client-side reserved usernames (mirrors server policy) to provide instant feedback
        const RESERVED = new Set([
            'admin','administrator','adminteam','admins','root','system','sysadmin','superadmin','superuser',
            'support','help','helpdesk','moderator','mod','mods','staff','team','security','official',
            'noreply','no-reply','postmaster','abuse','report','reports','owner','undefined','null'
        ]);
        if (RESERVED.has(username.toLowerCase())) {
            this.showAuthError('Username unavailable');
            this.app.showNotification('Username unavailable', 'error');
            return;
        }

        if (!username || !email || !password || !confirm) {
            this.showAuthError('Please fill in all fields');
            return;
        }

        // Require explicit agreement to Terms + Privacy
        const tos = document.getElementById('register-tos');
        if (tos && !tos.checked) {
            this.showAuthError('Please agree to the ToS and Privacy');
            return;
        }

        if (password !== confirm) {
            this.showAuthError('Passwords do not match');
            return;
        }

        const score = ((pwd) => {
            if (!pwd) return 0;
            let categories = 0;
            if (/[a-z]/.test(pwd)) categories++;
            if (/[A-Z]/.test(pwd)) categories++;
            if (/[0-9]/.test(pwd)) categories++;
            if (/[^A-Za-z0-9]/.test(pwd)) categories++;
            const long = pwd.length >= 8;
            if (!long) return 0;
            return Math.min(categories, 4);
        })(password);
        if (score < 3) {
            this.showAuthError('Password too weak. Use at least 8 chars and 3 of: upper, lower, number, symbol.');
            return;
        }

        this.app.showLoader();
        this.hideAuthError();

        // Preflight: if username already exists, short-circuit with friendly error
        try {
            const existsResp = await fetchWithCSRF(`/api/users/${encodeURIComponent(username)}`);
            if (existsResp && existsResp.ok) {
                this.app.hideLoader();
                this.showAuthError('Username unavailable');
                this.app.showNotification('Username unavailable', 'error');
                return;
            }
        } catch {}

        try {
            const response = await fetchWithCSRF('/api/register' + (invite ? ('?invite=' + encodeURIComponent(invite)) : ''), {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'include',
                body: JSON.stringify({ username, email, password, invite })
            });

            if (response.status === 401) {
                localStorage.removeItem('token');
                localStorage.removeItem('user');
            }

            const data = await response.json().catch(() => ({}));

            if (response.ok) {
                try { if (data && data.token) localStorage.setItem('token', data.token); } catch {}
                localStorage.setItem('user', JSON.stringify(data.user));
                this.currentUser = data.user;
                this.app.currentUser = data.user;
                this.closeAuthModal();
                this.updateAuthButton();
                this.app.showNotification(`Welcome to TROUGH, ${data.user.username}!`, 'success');
                this._pendingInvite = '';
                try {
                    history.pushState({}, '', `/@${encodeURIComponent(data.user.username)}`);
                    await this.app.renderProfilePage(data.user.username);
                } catch {}
            } else {
                const err = (data && typeof data.error === 'string') ? data.error : '';
                if (response.status === 409) {
                    if (/email/i.test(err)) {
                        this.showAuthError('Email already registered');
                        this.app.showNotification('Email already registered', 'error');
                    } else {
                        this.showAuthError('Username unavailable');
                        this.app.showNotification('Username unavailable', 'error');
                    }
                } else if (response.status === 400 && /\busername\b/i.test(err) && /(reserved|taken|unavailable)/i.test(err)) {
                    this.showAuthError('Username unavailable');
                    this.app.showNotification('Username unavailable', 'error');
                } else {
                    this.showAuthError(err || 'Registration failed');
                }
            }
        } catch (error) {
            console.error('Registration error:', error);
            this.showAuthError('Connection error. Please try again.');
        }

        this.app.hideLoader();
    }

    showAuthModal() {
        this.authModal.classList.add('active');
        document.body.style.overflow = 'hidden';
        // Ensure magnetic scroll disables while modal is open
        if (this.app.magneticScroll && this.app.magneticScroll.updateEnabledState) this.app.magneticScroll.updateEnabledState();

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
        if (this.app.magneticScroll && this.app.magneticScroll.updateEnabledState) this.app.magneticScroll.updateEnabledState();
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

    async fetchPasswordRequirements() {
        try {
            const r = await fetchWithCSRF('/api/password-requirements');
            if (!r.ok) return;
            const reqs = await r.json();
            const pwReqsDiv = document.getElementById('password-requirements');
            if (!pwReqsDiv) return;

            let html = '<ul class="password-requirements-list">';
            html += `<li>At least ${reqs.min_length} characters</li>`;
            if (reqs.require_upper) html += '<li>At least one uppercase letter</li>';
            if (reqs.require_lower) html += '<li>At least one lowercase letter</li>';
            if (reqs.require_number) html += '<li>At least one number</li>';
            if (reqs.require_special) html += `<li>At least one special character (${reqs.allowed_special})</li>`;
            html += '</ul>';
            pwReqsDiv.innerHTML = html;
        } catch (e) {
            console.error('Failed to fetch password requirements:', e);
        }
    }

    async openForgotPassword() {
        const overlay = document.createElement('div'); overlay.style.cssText='position:fixed;inset:0;z-index:3050;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
        const panel = document.createElement('div'); panel.style.cssText='max-width:420px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
        panel.innerHTML = `<div class="settings-label">Reset your password</div><input id="fp-email" type="email" class="settings-input" placeholder="Email"/><div class="settings-actions"><button id="fp-send" class="nav-btn">Send reset link</button></div>`;
        overlay.appendChild(panel); document.body.appendChild(overlay);
        overlay.addEventListener('click', (e)=>{ if(e.target===overlay) overlay.remove(); });
        panel.querySelector('#fp-send').onclick = async () => {
            const email = panel.querySelector('#fp-email').value.trim();
            if (!email) { this.app.showNotification('Enter your email','error'); return; }
            const btn = panel.querySelector('#fp-send');
            btn.disabled = true;
            const prevText = btn.textContent;
            btn.textContent = 'Sending…';
            try {
                const r = await fetchWithCSRF('/api/forgot-password', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ email }) });
                if (r.status===204) {
                    this.app.showNotification('Check your email');
                    overlay.remove();
                } else {
                    const e = await r.json().catch(()=>({}));
                    this.app.showNotification(e.error||'Unable to send','error');
                    btn.disabled = false;
                    btn.textContent = prevText;
                }
            } catch {
                this.app.showNotification('Unable to send','error');
                btn.disabled = false;
                btn.textContent = prevText;
            }
        };
    }
}
