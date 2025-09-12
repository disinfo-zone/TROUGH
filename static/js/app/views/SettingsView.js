import { fetchWithCSRF } from '../services/api.js';
import { escapeHTML } from '../utils.js';

export default class SettingsView {
    constructor(app) {
        this.app = app;
    }

    async render() {
        if (this.app.magneticScroll && this.app.magneticScroll.updateEnabledState) {
            this.app.magneticScroll.updateEnabledState();
        }
        if (!this.app.currentUser) {
            this.app.auth.showAuthModal();
            return;
        }
        let email = '';
        try {
            const resp = await fetchWithCSRF('/api/me/account', { credentials: 'include' });
            if (resp.ok) {
                const acc = await resp.json();
                email = acc.email || '';
            }
        } catch {}

        this.app.gallery.innerHTML = '';
        if (this.app.profileTop) this.app.profileTop.innerHTML = '';
        // Ensure reset page uses centered settings layout
        this.app.gallery.className = 'gallery settings-mode';
        this.app.gallery.classList.add('settings-mode');
        const wrap = document.createElement('div');
        wrap.className = 'settings-wrap';
        const avatarURL = (this.app.currentUser && this.app.currentUser.avatar_url) ? this.app.currentUser.avatar_url : '';
        const needVerify = !!this.app.currentUser && this.app.currentUser.email_verified === false;
        // Optional top-of-page verify banner
        const verifyBanner = needVerify ? `
          <section class="settings-group" style="border-color:var(--border-strong)">
            <div class="mono-col" style="display:flex;gap:10px;align-items:center;justify-content:space-between">
              <div style="font-family:var(--font-mono)">
                <div style="font-weight:700">Email not verified — uploads locked</div>
                <div style="color:var(--text-secondary);font-size:0.9em">Verify your email to upload. You can still collect images.</div>
              </div>
              <button id="settings-resend-verify" class="nav-btn">Resend verification</button>
            </div>
          </section>` : '';
        wrap.innerHTML = `
          ${verifyBanner}
          <section class="settings-group">
            <div class="settings-label">Profile</div>
            <div class="avatar-row" style="align-items:flex-start;gap:12px">
              <div class="avatar-preview" id="avatar-preview" style="background-image:url('${avatarURL}')"></div>
              <div style="display:grid;gap:10px;flex:1;min-width:0;overflow:hidden">
                <label class="settings-label">Username</label>
                <input type="text" id="settings-username" value="${escapeHTML(String(this.app.currentUser.username))}" minlength="3" maxlength="30" pattern="[a-z0-9]+" title="3–30 lowercase letters or numbers" class="settings-input"/>
                <div class="settings-actions" style="gap:8px;align-items:center">
                  <button id="btn-username" class="nav-btn">Change Username</button>
                  <small id="err-username" style="color:#ff5c5c"></small>
                </div>
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
                <input type="password" id="new-password" placeholder="New password" minlength="8" class="settings-input"/>
                <input type="password" id="new-password-confirm" placeholder="Confirm new password" minlength="8" class="settings-input"/>
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
        this.app.gallery.appendChild(wrap);

        // Footer with public pages
        try {
            const r = await fetchWithCSRF('/api/pages');
            const d = await r.json().catch(()=>({pages:[]}));
            const pages = Array.isArray(d.pages) ? d.pages : [];
            if (pages.length) {
                const footer = document.createElement('div');
                footer.style.cssText = 'margin:12px auto 0;max-width:980px;display:flex;gap:12px;flex-wrap:wrap;justify-content:center;opacity:.8';
                pages.forEach(p => {
                    const a = document.createElement('a');
                    a.href = '/' + String(p.slug||'').replace(/^\/+/, '');
                    a.className = 'link-btn';
                    a.textContent = String(p.title||'');
                    a.onclick = (e) => { e.preventDefault(); history.pushState({}, '', a.href); this.app.init(); };
                    footer.appendChild(a);
                });
                this.app.gallery.appendChild(footer);
            }
        } catch {}

        // Password strength + confirm
        const pw = document.getElementById('new-password');
        const pwc = document.getElementById('new-password-confirm');
        const bar = document.getElementById('pw-bar');
        // Replace single bar with 4 segments for settings too
        const pwStrength = document.getElementById('pw-strength');
        if (pwStrength && !pwStrength.querySelector('.pw-seg')) {
            pwStrength.innerHTML = '<div class="pw-seg"><div class="fill"></div></div><div class="pw-seg"><div class="fill"></div></div><div class="pw-seg"><div class="fill"></div></div><div class="pw-seg"><div class="fill"></div></div>';
        }
        const scorePassword = (pwd) => {
            if (!pwd) return 0;
            let categories = 0;
            if (/[a-z]/.test(pwd)) categories++;
            if (/[A-Z]/.test(pwd)) categories++;
            if (/[0-9]/.test(pwd)) categories++;
            if (/[^A-Za-z0-9]/.test(pwd)) categories++;
            const long = pwd.length >= 8;
            if (!long) return 0;
            return Math.min(categories, 4);
        };
        const renderBar = () => {
            const score = scorePassword(pw.value);
            const segs = pwStrength ? pwStrength.querySelectorAll('.pw-seg .fill') : [];
            segs.forEach((seg, i) => {
                const active = score >= (i + 1);
                seg.style.width = active ? '100%' : '0%';
                if (i === 3) seg.classList.toggle('shimmer', score === 4);
            });
        };
        pw.addEventListener('input', renderBar); renderBar();

        // Handlers remain (updated references)
        const authHeader = { 'Content-Type': 'application/json' };
        const pref = (this.app.currentUser?.nsfw_pref || ((this.app.currentUser?.show_nsfw) ? 'show' : 'hide'));
        (document.querySelector(`input[name='nsfw-pref'][value='${pref}']`)||document.querySelector(`input[name='nsfw-pref'][value='hide']`)).checked = true;
        document.getElementById('btn-nsfw').onclick = async () => {
            const sel = document.querySelector("input[name='nsfw-pref']:checked")?.value || 'hide';
            try { const resp = await fetchWithCSRF('/api/me/profile', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ nsfw_pref: sel }) }); if (!resp.ok) throw await resp.json(); const u = await resp.json(); this.app.currentUser = u; localStorage.setItem('user', JSON.stringify(u)); this.app.showNotification('NSFW preference saved'); } catch (e) { document.getElementById('err-nsfw').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-username').onclick = async () => {
            const inputEl = document.getElementById('settings-username');
            const errEl = document.getElementById('err-username');
            errEl.textContent = '';
            const raw = (inputEl.value || '').trim();
            const username = raw.toLowerCase();
            const current = (this.app.currentUser?.username || '').toLowerCase();
            const RESERVED = new Set(['admin','administrator','adminteam','admins','root','system','sysadmin','superadmin','superuser','support','help','helpdesk','moderator','mod','mods','staff','team','security','official','noreply','no-reply','postmaster','abuse','report','reports','owner','undefined','null']);
            if (!username) { errEl.textContent = 'Enter a username'; return; }
            if (RESERVED.has(username)) { errEl.textContent = 'Username unavailable'; this.app.showNotification('Username unavailable', 'error'); return; }
            if (username === current) { errEl.textContent = 'This is already your username'; return; }
            try {
                const r = await fetchWithCSRF(`/api/users/${encodeURIComponent(username)}`);
                if (r && r.ok) { errEl.textContent = 'Username unavailable'; this.app.showNotification('Username unavailable', 'error'); return; }
            } catch {}
            try {
                const resp = await fetchWithCSRF('/api/me/profile', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ username }) });
                if (!resp.ok) {
                    let data = {};
                    try { data = await resp.json(); } catch {}
                    const msg = (data && data.error) ? String(data.error) : '';
                    if (resp.status === 409 || (/\busername\b/i.test(msg) && /(taken|reserved|unavailable)/i.test(msg))) {
                        errEl.textContent = 'Username unavailable';
                        this.app.showNotification('Username unavailable', 'error');
                        return;
                    }
                    errEl.textContent = msg || 'Failed';
                    return;
                }
                const userResp = await resp.json();
                this.app.currentUser=userResp;
                localStorage.setItem('user', JSON.stringify(userResp));
                this.app.updateAuthButton();
                this.app.showNotification('Username changed');
            } catch (e) { errEl.textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-email').onclick = async () => {
            const v = document.getElementById('settings-email').value.trim();
            try { const resp = await fetchWithCSRF('/api/me/email', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ email: v }) }); if (!resp.ok) throw await resp.json(); this.app.showNotification('Email updated'); } catch (e) { document.getElementById('err-email').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-password').onclick = async () => {
            const current = document.getElementById('current-password').value; const next = pw.value; const confirm = pwc.value;
            if (next !== confirm) { document.getElementById('err-password').textContent = 'Passwords do not match'; return; }
            try { const resp = await fetchWithCSRF('/api/me/password', { method:'PATCH', headers: authHeader, body: JSON.stringify({ current_password: current, new_password: next }) }); if (resp.status !== 204) throw await resp.json(); document.getElementById('current-password').value=''; pw.value=''; pwc.value=''; renderBar(); this.app.showNotification('Password changed'); } catch (e) { document.getElementById('err-password').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-delete').onclick = async () => {
            const conf = document.getElementById('delete-confirm').value.trim(); if (conf !== 'DELETE') { document.getElementById('err-delete').textContent='Type DELETE to confirm'; return; }
            try { const resp = await fetchWithCSRF('/api/me', { method:'DELETE', headers: authHeader, body: JSON.stringify({ confirm:'DELETE' }) }); if (resp.status !== 204) throw await resp.json(); await this.app.auth.signOut(); window.location.href='/'; } catch (e) { document.getElementById('err-delete').textContent = e.error || 'Failed'; }
        };

        // Avatar upload
        document.getElementById('avatar-upload').onclick = async () => {
            const fileInput = document.getElementById('avatar-file'); const file = fileInput.files && fileInput.files[0]; if (!file) { this.app.showNotification('Choose a file first', 'error'); return; }
            const fd = new FormData(); fd.append('avatar', file);
            try {
                const resp = await fetchWithCSRF('/api/me/avatar', { method:'POST', credentials: 'include', body: fd });
                if (!resp.ok) throw await resp.json();
                const data = await resp.json();
                this.app.currentUser.avatar_url = data.avatar_url; localStorage.setItem('user', JSON.stringify(this.app.currentUser));
                const pv = document.getElementById('avatar-preview'); if (pv) { try { pv.style.backgroundImage = `url('${encodeURI(String(data.avatar_url||''))}')`; pv.style.display = 'block'; } catch {} }
                const navAvatar = document.querySelector('.nav-avatar'); if (navAvatar) { try { navAvatar.style.backgroundImage = `url('${encodeURI(String(data.avatar_url||''))}')`; } catch {} }
                const headerAv = document.querySelector('.avatar-preview'); if (headerAv) { try { headerAv.style.backgroundImage = `url('${encodeURI(String(data.avatar_url||''))}')`; } catch {} }
                const authBtnAvatar = this.app.authBtn.querySelector('.avatar'); if (authBtnAvatar) { try { authBtnAvatar.style.backgroundImage = `url('${encodeURI(String(data.avatar_url||''))}')`; } catch {} }
                this.app.showNotification('Avatar updated');
            } catch (e) {
                console.error('Avatar upload error:', e);
                const errorMsg = e.error || (typeof e === 'string' ? e : 'Upload failed');
                this.app.showNotification(errorMsg, 'error');
            }
        };
        if (needVerify) {
            const btn1 = document.getElementById('btn-resend-verify');
            if (btn1) btn1.onclick = async () => {
                try { const r = await fetchWithCSRF('/api/me/resend-verification', { method:'POST', credentials:'include' }); if (r.status===204) this.app.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Unable to send','error'); } } catch {}
            };
            const btn2 = document.getElementById('settings-resend-verify');
            if (btn2) btn2.onclick = async () => {
                try { const r = await fetchWithCSRF('/api/me/resend-verification', { method:'POST', credentials:'include' }); if (r.status===204) this.app.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Unable to send','error'); } } catch {}
            };
        }
    }
}
