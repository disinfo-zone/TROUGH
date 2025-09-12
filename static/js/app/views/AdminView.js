import { fetchWithCSRF } from '../services/api.js';
import { escapeHTML } from '../utils.js';

export default class AdminView {
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
        const isAdmin = !!this.app.currentUser.is_admin;
        const isModerator = !!this.app.currentUser.is_moderator;
        if (!isAdmin && !isModerator) {
            this.app.showNotification('Forbidden', 'error');
            history.replaceState({}, '', `/@${encodeURIComponent(this.app.currentUser.username)}`);
            return;
        }

        if (this.app.profileTop) this.app.profileTop.innerHTML = '';
        this.app.gallery.innerHTML = '';
        this.app.gallery.classList.add('settings-mode');

        const wrap = document.createElement('div');
        wrap.className = 'settings-wrap';

        const siteSection = document.createElement('section');
        siteSection.className = 'settings-group';
        if (isAdmin) {
            let s = {};
            try {
                const r = await fetchWithCSRF('/api/admin/site', { credentials: 'include' });
                if (r.ok) s = await r.json();
            } catch {}
            const smtpConfigured = !!(s.smtp_host && s.smtp_port && s.smtp_username && s.smtp_password);
            siteSection.innerHTML = `
              <div class="settings-label">Site settings</div>
              <input id="site-name" class="settings-input" placeholder="Site name" value="${s.site_name||''}"/>
              <input id="site-url" class="settings-input" placeholder="Site URL" value="${s.site_url||''}"/>
              <input id="seo-title" class="settings-input" placeholder="SEO title" value="${s.seo_title||''}"/>
              <textarea id="seo-description" class="settings-input" placeholder="SEO description">${s.seo_description||''}</textarea>
              <div class="settings-label">Favicon</div>
              <div class="settings-actions" style="gap:8px;align-items:center">
                <input id="favicon-file" type="file" accept="image/x-icon,image/vnd.microsoft.icon,image/png,image/svg+xml"/>
                <button id="btn-upload-favicon" class="nav-btn">Upload favicon</button>
                <img id="favicon-preview" src="${s.favicon_path||''}" alt="Favicon preview" style="height:24px;width:24px;object-fit:contain;border:1px solid var(--border);border-radius:4px;${s.favicon_path?'':'display:none'}"/>
              </div>
              <div class="settings-label">Social image</div>
              <input id="social-image" class="settings-input" placeholder="Social image URL" value="${s.social_image_url||''}"/>
              <div class="settings-actions" style="gap:8px;align-items:center">
                <input id="social-image-file" type="file" accept="image/*"/>
                <button id="btn-upload-social" class="nav-btn">Upload social image</button>
                <img id="social-image-preview" src="${s.social_image_url||''}" alt="Social image preview" style="height:40px;aspect-ratio:1/1;object-fit:cover;border:1px solid var(--border);border-radius:8px;${s.social_image_url?'':'display:none'}"/>
              </div>
              <div class="settings-label">Registration</div>
              <label style="display:flex;gap:8px;align-items:center"><input id="public-reg" type="checkbox" ${s.public_registration_enabled!==false?'checked':''}/> Allow public registration</label>
              <div class="settings-label" style="margin-top:8px">Analytics</div>
              <label style="display:flex;gap:8px;align-items:center;margin-bottom:4px"><input id="analytics-enabled" type="checkbox" ${s.analytics_enabled?'checked':''}/> Enable site analytics</label>
              <div id="analytics-config" style="display:${s.analytics_enabled?'grid':'none'};gap:8px">
                <div style="display:grid;gap:6px"><label class="settings-label">Provider</label>
                  <select id="analytics-provider" class="settings-input">
                    <option value="" ${!s.analytics_provider? 'selected':''}>Select provider</option>
                    <option value="ga4" ${s.analytics_provider==='ga4'?'selected':''}>Google Analytics 4</option>
                    <option value="umami" ${s.analytics_provider==='umami'?'selected':''}>Umami (self-hosted)</option>
                    <option value="plausible" ${s.analytics_provider==='plausible'?'selected':''}>Plausible (self-hosted)</option>
                  </select>
                </div>
                <div id="ga4-fields" style="display:${s.analytics_provider==='ga4'?'grid':'none'};gap:8px">
                  <input id="ga4-id" class="settings-input" placeholder="GA4 Measurement ID (e.g., G-XXXXXXX)" value="${s.ga4_measurement_id||''}"/>
                </div>
                <div id="umami-fields" style="display:${s.analytics_provider==='umami'?'grid':'none'};gap:8px">
                  <input id="umami-src" class="settings-input" placeholder="Umami script URL (https://domain/script.js)" value="${s.umami_src||''}"/>
                  <input id="umami-website-id" class="settings-input" placeholder="Website ID (UUID)" value="${s.umami_website_id||''}"/>
                </div>
                <div id="plausible-fields" style="display:${s.analytics_provider==='plausible'?'grid':'none'};gap:8px">
                  <input id="plausible-src" class="settings-input" placeholder="Plausible script URL (https://domain/js/script.js)" value="${s.plausible_src||''}"/>
                  <input id="plausible-domain" class="settings-input" placeholder="Your site domain (example.com)" value="${s.plausible_domain||''}"/>
                </div>
              </div>
              <div class="settings-actions"><button id="btn-save-site-core" class="nav-btn">Save site settings</button></div>
              <div class="settings-label" style="display:flex;align-items:center;justify-content:space-between">
                <span>Storage settings (advanced)</span>
                <button id="toggle-storage" class="link-btn" title="Advanced: change only if you know what you're doing">Show</button>
              </div>
              <div id="storage-section" style="display:none;gap:8px">
                <div style="display:grid;gap:8px">
                <label class="settings-label">Provider</label>
                <select id="storage-provider" class="settings-input">
                  <option value="local" ${!s.storage_provider || s.storage_provider==='local' ? 'selected' : ''}>Local</option>
                  <option value="s3" ${s.storage_provider==='s3' || s.storage_provider==='r2' ? 'selected' : ''}>S3 / R2</option>
                </select>
                <div id="s3-advanced" style="display:${(s.storage_provider==='s3'||s.storage_provider==='r2')?'grid':'none'};gap:8px">
                  <input id="s3-endpoint" class="settings-input" placeholder="S3/R2 endpoint (https://...)" value="${s.s3_endpoint||''}"/>
                  <input id="s3-bucket" class="settings-input" placeholder="Bucket name" value="${s.s3_bucket||''}"/>
                  <input id="s3-access" class="settings-input" placeholder="Access key" value="${s.s3_access_key||''}"/>
                  <input id="s3-secret" class="settings-input" type="password" placeholder="Secret key" value="${s.s3_secret_key||''}"/>
                  <label style="display:flex;gap:8px;align-items:center"><input id="s3-path" type="checkbox" ${s.s3_force_path_style?'checked':''}/> Force path-style URLs</label>
                  <input id="public-base" class="settings-input" placeholder="Public base URL (e.g., CDN)" value="${s.public_base_url||''}"/>
                </div>
                <div class="settings-actions" style="gap:8px;align-items:center">
                  <span id="storage-status" class="meta" style="opacity:.8">Current: ${s.storage_provider||'local'}</span>
                  <button id="btn-test-storage" class="nav-btn">Verify storage</button>
                </div>
                <div class="settings-actions" style="gap:8px;align-items:center">
                  <button id="btn-save-storage" class="nav-btn">Save Storage Settings</button>
                  <button id="btn-export-upload" class="nav-btn">Migrate to Remote Storage</button>
                </div>
                </div>
              </div>
              <div class="settings-label" style="display:flex;align-items:center;justify-content:space-between">
                <span>SMTP settings (advanced)</span>
                <button id="toggle-smtp" class="link-btn" title="Advanced: change only if you know what you're doing">Show</button>
              </div>
              <div id="smtp-section" style="display:none;gap:8px">
                <input id="smtp-host" class="settings-input" placeholder="SMTP host (hostname only, no http/https)" value="${s.smtp_host||''}"/>
                <input id="smtp-port" class="settings-input no-spinner" type="number" placeholder="SMTP port" value="${s.smtp_port||''}"/>
                <input id="smtp-username" class="settings-input" placeholder="SMTP username (often your full email address)" value="${s.smtp_username||''}"/>
                <input id="smtp-password" class="settings-input" type="password" placeholder="SMTP password" value="${s.smtp_password||''}"/>
                <input id="smtp-from" class="settings-input" placeholder="From email (optional, defaults to username)" value="${s.smtp_from_email||''}"/>
                <label style="display:flex;gap:8px;align-items:center"><input id="smtp-tls" type="checkbox" ${s.smtp_tls?'checked':''}/> Use TLS (465 implicit TLS or 587 STARTTLS)</label>
                ${smtpConfigured ? `<label style=\"display:flex;gap:8px;align-items:center\"><input id=\"require-verify\" type=\"checkbox\" ${s.require_email_verification?'checked':''}/> Require email verification for new accounts</label>
                <div class="settings-actions" style="gap:8px;align-items:center"><input id="smtp-test-to" class="settings-input" placeholder="Test email to"/><button id="btn-smtp-test" class="nav-btn">Send test</button></div>` : '<small style="color:var(--text-tertiary)">Enter SMTP settings to enable email features</small>'}
                <div class="settings-actions" style="gap:8px;align-items:center;margin-top:8px"><button id="btn-save-site" class="nav-btn">Save SMTP settings</button></div>
              </div>`;

            const favInput = siteSection.querySelector('#favicon-file');
            const favPreview = siteSection.querySelector('#favicon-preview');
            if (favInput) favInput.onchange = () => { const f = favInput.files && favInput.files[0]; if (f) { favPreview.src = URL.createObjectURL(f); favPreview.style.display='inline-block'; } };

            const upFavBtn = siteSection.querySelector('#btn-upload-favicon');
            if (upFavBtn) upFavBtn.onclick = async () => {
                const f = favInput.files[0]; if (!f) { this.app.showNotification('Choose a favicon file', 'error'); return; }
                const fd = new FormData(); fd.append('favicon', f);
                const r = await fetchWithCSRF('/api/admin/site/favicon', { method:'POST', credentials:'include', body: fd });
                if (r.ok) { const d = await r.json(); favPreview.src = d.favicon_path || favPreview.src; favPreview.style.display='inline-block'; this.app.showNotification('Favicon uploaded'); await this.app.applyPublicSiteSettings(); }
                else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Upload failed','error'); }
            };

            const socialInput = siteSection.querySelector('#social-image-file');
            const socialPreview = siteSection.querySelector('#social-image-preview');
            if (socialInput) socialInput.onchange = () => { const f = socialInput.files && socialInput.files[0]; if (f) { socialPreview.src = URL.createObjectURL(f); socialPreview.style.display='inline-block'; } };

            const upSocialBtn = siteSection.querySelector('#btn-upload-social');
            if (upSocialBtn) upSocialBtn.onclick = async () => {
                const f = socialInput.files[0]; if (!f) { this.app.showNotification('Choose a social image file', 'error'); return; }
                const fd = new FormData(); fd.append('image', f);
                const r = await fetchWithCSRF('/api/admin/site/social-image', { method:'POST', credentials:'include', body: fd });
                if (r.ok) { const d = await r.json(); siteSection.querySelector('#social-image').value = d.social_image_url || ''; socialPreview.src = d.social_image_url || socialPreview.src; socialPreview.style.display='inline-block'; this.app.showNotification('Social image uploaded'); }
                else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Upload failed','error'); }
            };

            const analyticsEnabled = siteSection.querySelector('#analytics-enabled');
            const analyticsConfig = siteSection.querySelector('#analytics-config');
            const analyticsProviderSel = siteSection.querySelector('#analytics-provider');
            const showByProvider = () => {
                const p = (analyticsProviderSel?.value||'').toLowerCase();
                const show = (id, on) => { const el = siteSection.querySelector('#'+id); if (el) el.style.display = on ? 'grid' : 'none'; };
                show('ga4-fields', p==='ga4');
                show('umami-fields', p==='umami');
                show('plausible-fields', p==='plausible');
            };
            if (analyticsEnabled) analyticsEnabled.onchange = () => {
                if (analyticsConfig) analyticsConfig.style.display = analyticsEnabled.checked ? 'grid' : 'none';
                if (analyticsEnabled.checked && analyticsProviderSel && !analyticsProviderSel.value) analyticsProviderSel.focus();
            };
            if (analyticsProviderSel) analyticsProviderSel.onchange = showByProvider;
            showByProvider();
        }

        const pagesSection = document.createElement('section');
        pagesSection.className = 'settings-group';
        pagesSection.innerHTML = `
          <div class="settings-label" style="display:flex;align-items:center;justify-content:space-between"><span>Add/Edit Pages</span> <small class="meta" style="opacity:.8">Single-segment slugs only (e.g., about, faq)</small></div>
          <div style="display:grid;gap:8px">
            <div style="display:grid;gap:6px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
              <div style="display:grid;gap:6px">
                <label class="settings-label">Slug</label>
                <input id="pg-slug" class="settings-input" placeholder="e.g., about"/>
              </div>
              <div style="display:grid;gap:6px">
                <label class="settings-label">Title</label>
                <input id="pg-title" class="settings-input" placeholder="Page title"/>
              </div>
              <div style="display:grid;gap:6px">
                <label class="settings-label">Redirect URL (optional)</label>
                <input id="pg-redirect" class="settings-input" placeholder="https://external.example/path"/>
              </div>
            </div>
            <div style="display:grid;gap:6px">
              <label class="settings-label">Content (Markdown)</label>
              <textarea id="pg-markdown" class="settings-input" style="min-height:200px" placeholder="# Heading\n\nWrite your content here..."></textarea>
              <small class="meta" style="opacity:.8">Supports GitHub-flavored markdown. Links open in a new tab.</small>
            </div>
            <div style="display:grid;gap:6px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
              <div style="display:grid;gap:6px"><label class="settings-label">Meta title (optional)</label><input id="pg-meta-title" class="settings-input" placeholder="Overrides <title>"/></div>
              <div style="display:grid;gap:6px"><label class="settings-label">Meta description (optional)</label><input id="pg-meta-desc" class="settings-input" placeholder="Short description for SEO"/></div>
            </div>
            <label style="display:flex;gap:8px;align-items:center"><input id="pg-published" type="checkbox"/> Published</label>
            <div class="settings-actions" style="gap:8px;align-items:center">
              <button id="pg-save" class="nav-btn">Save</button>
              <button id="pg-new" class="link-btn">New</button>
              <button id="pg-delete" class="link-btn" style="color:#ff6666">Delete</button>
            </div>
          </div>
          <div id="pg-list" style="display:grid;gap:6px;margin-top:8px"></div>
        `;

        const invitesSection = document.createElement('section');
        invitesSection.className = 'settings-group';
        invitesSection.innerHTML = `
          <div class="settings-label">Invitations</div>
          <div style="display:grid;gap:8px;margin-bottom:8px">
            <div class="meta" style="opacity:.8">Create a new invite code. You can specify the number of uses and/or a validity period. Leave a field empty for no limit.</div>
            <div style="display:grid;gap:8px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
              <div style="display:grid;gap:6px"><label class="settings-label" for="inv-max-uses">Max uses</label><input id="inv-max-uses" class="settings-input no-spinner" type="number" min="0" placeholder="0 = unlimited"/></div>
              <div style="display:grid;gap:6px"><label class="settings-label" for="inv-duration">Validity</label><input id="inv-duration" class="settings-input" placeholder="e.g., 24h or 7d (blank = no expiration)"/></div>
            </div>
            <div class="settings-actions" style="gap:8px;align-items:center">
              <button id="btn-create-invite" class="nav-btn">Create invite</button>
            </div>
          </div>
          <div id="invite-list" style="display:grid;gap:8px"></div>
          <div id="invite-pagination" class="pager">
            <div class="pager-controls">
              <button id="inv-prev" class="nav-btn" disabled>Prev</button>
              <button id="inv-next" class="nav-btn" disabled>Next</button>
            </div>
            <div id="inv-page-info" class="meta" style="opacity:.8"></div>
          </div>
        `;

        const usersSection = document.createElement('section');
        usersSection.className = 'settings-group';
        usersSection.innerHTML = `
          <div class="settings-label">User management</div>
          <input id="user-search" class="settings-input" placeholder="Search users by name or email"/>
          <div id="user-results" class="user-results"></div>
          <div id="user-pagination" class="pager">
            <div class="pager-controls">
              <button id="user-prev" class="nav-btn" disabled>Prev</button>
              <button id="user-next" class="nav-btn" disabled>Next</button>
            </div>
            <div id="user-page-info" class="meta" style="opacity:.8"></div>
          </div>
        `;

        wrap.appendChild(siteSection);
        const tabsWrap = document.createElement('div');
        tabsWrap.className = 'tab-group admin-tabs';
        tabsWrap.setAttribute('role', 'tablist');
        tabsWrap.style.cssText = 'margin:0 auto 12px;';
        const mkTab = (id, label) => { const b = document.createElement('button'); b.className='tab-btn'; b.dataset.tab=id; b.textContent=label; b.setAttribute('role','tab'); b.setAttribute('aria-selected','false'); b.setAttribute('aria-pressed','false'); return b; };
        const tabSite = mkTab('site', 'Site settings');
        const tabPages = isAdmin ? mkTab('pages', 'Add/Edit Pages') : null;
        const tabInv = mkTab('invites', 'Invitations');
        const tabUsers = mkTab('users', 'User management');
        const tabBackups = isAdmin ? mkTab('backups', 'Backups') : null;
        tabsWrap.appendChild(tabSite);
        if (tabPages) tabsWrap.appendChild(tabPages);
        tabsWrap.appendChild(tabInv);
        tabsWrap.appendChild(tabUsers);
        if (tabBackups) tabsWrap.appendChild(tabBackups);
        wrap.appendChild(tabsWrap);
        const sections = document.createElement('div');
        sections.appendChild(siteSection);
        if (isAdmin) sections.appendChild(pagesSection);
        sections.appendChild(invitesSection);
        sections.appendChild(usersSection);
        let backupsSection = null;
        if (isAdmin) {
            backupsSection = document.createElement('section');
            backupsSection.className = 'settings-group';
            backupsSection.innerHTML = `
              <div class="settings-label" style="display:flex;align-items:center;justify-content:space-between"><span>Backups</span><small class="meta" style="opacity:.8">Database only; images are not included</small></div>
              <div style="display:grid;gap:8px">
                <div class="settings-actions" style="gap:8px;align-items:center">
                  <button id="btn-backup-download" class="nav-btn">Create & download backup</button>
                  <button id="btn-backup-save" class="nav-btn">Create & save on server</button>
                </div>
                <div style="display:grid;gap:8px">
                  <label class="settings-label">Restore</label>
                  <input id="backup-file" type="file" accept=".gz,.json"/>
                  <button id="btn-backup-restore" class="nav-btn">Restore from file</button>
                </div>
                <div style="display:grid;gap:8px">
                  <label class="settings-label">Automatic backups</label>
                  <label style="display:flex;gap:8px;align-items:center"><input id="backup-enabled" type="checkbox"/> Enable scheduler</label>
                  <div style="display:grid;gap:6px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
                    <div style="display:grid;gap:6px"><label class="settings-label">Interval</label><input id="backup-interval" class="settings-input" placeholder="e.g., 24h, 7h"/></div>
                    <div style="display:grid;gap:6px"><label class="settings-label">Keep days</label><input id="backup-keep" class="settings-input no-spinner" type="number" min="1"/></div>
                  </div>
                  <div class="settings-actions" style="gap:8px;align-items:center"><button id="btn-save-backup-settings" class="nav-btn">Save backup settings</button></div>
                </div>
                <div style="display:grid;gap:8px">
                  <label class="settings-label">Saved backups</label>
                  <div id="backup-list" style="display:grid;gap:6px"></div>
                </div>
              </div>`;
            sections.appendChild(backupsSection);
        }
        wrap.appendChild(sections);
        const showSection = (name) => {
            const map = { site: siteSection, pages: pagesSection, invites: invitesSection, users: usersSection, backups: backupsSection };
            [siteSection, pagesSection, invitesSection, usersSection, backupsSection].forEach(sec => { if (sec) sec.style.display = 'none'; });
            if (map[name]) map[name].style.display = 'block';
            const setActive = (btn, on) => {
                if (!btn) return;
                btn.setAttribute('aria-selected', on ? 'true' : 'false');
                btn.setAttribute('aria-pressed', on ? 'true' : 'false');
                if (on) {
                    btn.classList.add('active');
                    try { btn.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' }); } catch { try { btn.scrollIntoView(); } catch {} }
                } else {
                    btn.classList.remove('active');
                }
            };
            setActive(tabSite, name==='site'); setActive(tabPages, name==='pages'); setActive(tabInv, name==='invites'); setActive(tabUsers, name==='users'); setActive(tabBackups, name==='backups');
        };
        showSection('site');
        tabSite.onclick = () => showSection('site');
        if (tabPages) tabPages.onclick = () => showSection('pages');
        tabInv.onclick = () => showSection('invites');
        tabUsers.onclick = () => showSection('users');
        if (tabBackups) tabBackups.onclick = () => showSection('backups');

        this.app.gallery.appendChild(wrap);

        if (isAdmin) {
            const initBackups = async () => {
                try {
                    const rs = await fetchWithCSRF('/api/admin/site', { credentials:'include' });
                    const s = rs.ok ? await rs.json() : {};
                    const be = backupsSection.querySelector('#backup-enabled');
                    const bi = backupsSection.querySelector('#backup-interval');
                    const bk = backupsSection.querySelector('#backup-keep');
                    if (be) be.checked = !!s.backup_enabled;
                    if (bi) bi.value = s.backup_interval || '24h';
                    if (bk) bk.value = s.backup_keep_days || 7;
                } catch {}
                const listEl = backupsSection.querySelector('#backup-list');
                const loadList = async () => {
                    const r = await fetchWithCSRF('/api/admin/backups', { credentials:'include' });
                    listEl.innerHTML = '';
                    if (!r.ok) return;
                    const d = await r.json().catch(()=>({backups:[]}));
                    (d.backups||[]).forEach(f => {
                        const row = document.createElement('div');
                        row.style.cssText = 'display:grid;grid-template-columns:1fr auto auto;gap:8px;align-items:center;border:1px solid var(--border);border-radius:8px;padding:8px;';
                        const sizeMB = (f.size/1024/1024).toFixed(2);
                        row.innerHTML = `<div><div style="font-weight:600">${escapeHTML(String(f.name||''))}</div><div class="meta" style="opacity:.8">${sizeMB} MB • ${new Date(f.mod_time).toLocaleString()}</div></div><button class="nav-btn" data-act="download">Download</button><button class="nav-btn nav-btn-danger" data-act="remove">Delete</button>`;
                        row.querySelector('[data-act="download"]').onclick = () => {
                            const a = document.createElement('a'); a.href = `/api/admin/backups/${encodeURIComponent(f.name)}`; a.download = f.name; document.body.appendChild(a); a.click(); a.remove();
                        };
                        row.querySelector('[data-act="remove"]').onclick = async () => {
                            const ok = await this.app.showConfirm('Delete this backup?'); if (!ok) return;
                            const rr = await fetchWithCSRF(`/api/admin/backups/${encodeURIComponent(f.name)}`, { method:'DELETE', credentials:'include' });
                            if (rr.status===204) { this.app.showNotification('Deleted'); loadList(); } else { this.app.showNotification('Delete failed','error'); }
                        };
                        listEl.appendChild(row);
                    });
                };
                await loadList();
                const dlBtn = backupsSection.querySelector('#btn-backup-download');
                if (dlBtn) dlBtn.onclick = async () => {
                    try {
                        const r = await fetchWithCSRF('/api/admin/backups/download', { method:'POST', credentials:'include' });
                        if (!r.ok) { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Failed','error'); return; }
                        const blob = await r.blob();
                        const cd = r.headers.get('Content-Disposition')||'';
                        const name = (/filename="?([^";]+)"?/i.exec(cd)||[])[1] || `trough-backup-${Date.now()}.json.gz`;
                        const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = name; document.body.appendChild(a); a.click(); a.remove();
                    } catch { this.app.showNotification('Failed','error'); }
                };
                const saveBtn = backupsSection.querySelector('#btn-backup-save');
                if (saveBtn) saveBtn.onclick = async () => {
                    const r = await fetchWithCSRF('/api/admin/backups/save', { method:'POST', credentials:'include' });
                    if (r.ok) { this.app.showNotification('Saved'); await loadList(); }
                    else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Failed','error'); }
                };
                const restoreBtn = backupsSection.querySelector('#btn-backup-restore');
                const fileInp = backupsSection.querySelector('#backup-file');
                if (restoreBtn) restoreBtn.onclick = async () => {
                    const f = fileInp && fileInp.files && fileInp.files[0]; if (!f) { this.app.showNotification('Choose a backup file','error'); return; }
                    const ok = await this.app.showConfirm('Restore will replace existing data. Continue?'); if (!ok) return;
                    const fd = new FormData(); fd.append('file', f);
                    const r = await fetchWithCSRF('/api/admin/backups/restore', { method:'POST', credentials:'include', body: fd });
                    if (r.status===204) { this.app.showNotification('Restored'); }
                    else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Restore failed','error'); }
                };
                const saveSettingsBtn = backupsSection.querySelector('#btn-save-backup-settings');
                if (saveSettingsBtn) saveSettingsBtn.onclick = async () => {
                    const rs = await fetchWithCSRF('/api/admin/site', { credentials:'include' });
                    const s = rs.ok ? await rs.json() : {};
                    const body = {
                        site_name: s.site_name||'', site_url: s.site_url||'', seo_title: s.seo_title||'', seo_description: s.seo_description||'', social_image_url: s.social_image_url||'',
                        storage_provider: s.storage_provider||'local', s3_endpoint: s.s3_endpoint||'', s3_bucket: s.s3_bucket||'', s3_access_key: s.s3_access_key||'', s3_secret_key: s.s3_secret_key||'', s3_force_path_style: !!s.s3_force_path_style, public_base_url: s.public_base_url||'',
                        smtp_host: s.smtp_host||'', smtp_port: s.smtp_port||0, smtp_username: s.smtp_username||'', smtp_password: s.smtp_password||'', smtp_from_email: s.smtp_from_email||'', smtp_tls: !!s.smtp_tls,
                        require_email_verification: !!s.require_email_verification, public_registration_enabled: s.public_registration_enabled!==false,
                        analytics_enabled: !!s.analytics_enabled, analytics_provider: s.analytics_provider||'', ga4_measurement_id: s.ga4_measurement_id||'', umami_src: s.umami_src||'', umami_website_id: s.umami_website_id||'', plausible_src: s.plausible_src||'', plausible_domain: s.plausible_domain||'',
                        backup_enabled: backupsSection.querySelector('#backup-enabled')?.checked || false,
                        backup_interval: backupsSection.querySelector('#backup-interval')?.value || '24h',
                        backup_keep_days: parseInt(backupsSection.querySelector('#backup-keep')?.value||'7',10)
                    };
                    const r = await fetchWithCSRF('/api/admin/site', { method:'PUT', headers:{'Content-Type':'application/json'}, credentials:'include', body: JSON.stringify(body) });
                    if (r.ok) { this.app.showNotification('Saved'); }
                    else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Save failed','error'); }
                };
            };
            if (backupsSection) { initBackups(); }
            const pgSlug = pagesSection.querySelector('#pg-slug');
            const pgTitle = pagesSection.querySelector('#pg-title');
            const pgRedirect = pagesSection.querySelector('#pg-redirect');
            const pgMarkdown = pagesSection.querySelector('#pg-markdown');
            const pgMetaTitle = pagesSection.querySelector('#pg-meta-title');
            const pgMetaDesc = pagesSection.querySelector('#pg-meta-desc');
            const pgPub = pagesSection.querySelector('#pg-published');
            const pgSave = pagesSection.querySelector('#pg-save');
            const pgNew = pagesSection.querySelector('#pg-new');
            const pgDel = pagesSection.querySelector('#pg-delete');
            const pgList = pagesSection.querySelector('#pg-list');
            let selectedId = null;
            const slugRe = /^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$/;
            const loadPages = async (page=1) => {
                const r = await fetchWithCSRF(`/api/admin/pages?page=${page}&limit=200`, { credentials:'include' });
                if (!r.ok) { this.app.showNotification('Failed to load pages','error'); return; }
                const d = await r.json().catch(()=>({pages:[]}));
                pgList.innerHTML = '';
                (d.pages||[]).forEach(p => {
                    const row = document.createElement('div');
                    row.style.cssText = 'display:grid;grid-template-columns:1fr auto auto;gap:8px;align-items:center;border:1px solid var(--border);border-radius:8px;padding:8px;';
                    row.innerHTML = `<div><div style="font-weight:600">${escapeHTML(String(p.title||''))}</div><div class="meta" style="opacity:.8">/${escapeHTML(String(p.slug||''))} ${p.is_published?'• Published':''}</div></div><button class="nav-btn" data-act="edit">Edit</button><button class="nav-btn nav-btn-danger" data-act="remove">Delete</button>`;
                    row.querySelector('[data-act="edit"]').onclick = () => {
                        selectedId = p.id; pgSlug.value = p.slug||''; pgTitle.value = p.title||''; pgMarkdown.value = p.markdown||''; pgRedirect.value = p.redirect_url||''; pgMetaTitle.value = p.meta_title||''; pgMetaDesc.value = p.meta_description||''; pgPub.checked = !!p.is_published;
                    };
                    row.querySelector('[data-act="remove"]').onclick = async () => {
                        const ok = await this.app.showConfirm('Delete this page?'); if (!ok) return;
                        const rr = await fetchWithCSRF(`/api/admin/pages/${p.id}`, { method:'DELETE', credentials:'include' });
                        if (rr.status===204) { this.app.showNotification('Deleted'); loadPages(1); if (selectedId===p.id) { selectedId=null; pgNew.click(); } }
                        else { const e = await rr.json().catch(()=>({})); this.app.showNotification(e.error||'Delete failed','error'); }
                    };
                    pgList.appendChild(row);
                });
            };
            pgNew.onclick = () => { selectedId = null; pgSlug.value=''; pgTitle.value=''; pgRedirect.value=''; pgMarkdown.value=''; pgMetaTitle.value=''; pgMetaDesc.value=''; pgPub.checked=false; };
            pgDel.onclick = async () => { if (!selectedId) return; const ok = await this.app.showConfirm('Delete this page?'); if (!ok) return; const r = await fetchWithCSRF(`/api/admin/pages/${selectedId}`, { method:'DELETE', credentials:'include' }); if (r.status===204) { this.app.showNotification('Deleted'); selectedId=null; pgNew.click(); loadPages(1); } else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Delete failed','error'); } };
            pgSave.onclick = async () => {
                const slug = (pgSlug.value||'').trim().toLowerCase();
                if (!slugRe.test(slug)) { this.app.showNotification('Invalid slug','error'); return; }
                const body = {
                    slug,
                    title: (pgTitle.value||'').trim(),
                    markdown: (pgMarkdown.value||'').replace(/\r\n/g,'\n'),
                    is_published: !!pgPub.checked,
                    redirect_url: (pgRedirect.value||'').trim() || null,
                    meta_title: (pgMetaTitle.value||'').trim() || null,
                    meta_description: (pgMetaDesc.value||'').trim() || null,
                };
                const method = selectedId ? 'PUT' : 'POST';
                const url = selectedId ? `/api/admin/pages/${selectedId}` : '/api/admin/pages';
                const r = await fetchWithCSRF(url, { method, headers:{'Content-Type':'application/json'}, credentials:'include', body: JSON.stringify(body) });
                if (r.ok || r.status===201) { this.app.showNotification('Saved'); loadPages(1); }
                else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Save failed','error'); }
            };
            await loadPages(1);
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
                    public_registration_enabled: document.getElementById('public-reg')?.checked !== false,
                    analytics_enabled: document.getElementById('analytics-enabled')?.checked || false,
                    analytics_provider: document.getElementById('analytics-provider')?.value || '',
                    ga4_measurement_id: document.getElementById('ga4-id')?.value || '',
                    umami_src: document.getElementById('umami-src')?.value || '',
                    umami_website_id: document.getElementById('umami-website-id')?.value || '',
                    plausible_src: document.getElementById('plausible-src')?.value || '',
                    plausible_domain: document.getElementById('plausible-domain')?.value || '',
                };
                const r = await fetchWithCSRF('/api/admin/site', { method:'PUT', headers: { 'Content-Type':'application/json' }, credentials: 'include', body: JSON.stringify(body) });
                if (r.ok) { this.app.showNotification('Saved'); await this.app.applyPublicSiteSettings(); }
                else { this.app.showNotification('Save failed','error'); }
            };
            const toggleSection = (btnId, sectionId) => {
                const btn = document.getElementById(btnId);
                const sec = document.getElementById(sectionId);
                if (!btn || !sec) return;
                btn.onclick = () => {
                    const isHidden = sec.style.display === 'none' || sec.style.display === '';
                    sec.style.display = isHidden ? 'block' : 'none';
                    btn.textContent = isHidden ? 'Hide' : 'Show';
                };
            };
            toggleSection('toggle-storage', 'storage-section');
            toggleSection('toggle-smtp', 'smtp-section');

            const providerSel = document.getElementById('storage-provider');
            const s3Adv = document.getElementById('s3-advanced');
            if (providerSel && s3Adv) {
                providerSel.onchange = () => {
                    const v = providerSel.value;
                    s3Adv.style.display = (v === 's3' || v === 'r2') ? 'grid' : 'none';
                };
            }

            let invPage = 1; const invLimit = 50;
            const invList = document.getElementById('invite-list');
            const invPrev = document.getElementById('inv-prev');
            const invNext = document.getElementById('inv-next');
            const invInfo = document.getElementById('inv-page-info');
            const siteURL = (document.getElementById('site-url').value||'').trim();
            const buildLink = (code) => {
                const base = siteURL || (location.origin);
                return base.replace(/\/$/, '') + '/register?invite=' + code;
            };
            const copyToClipboard = async (text) => {
                try { await navigator.clipboard.writeText(text); this.app.showNotification('Copied'); } catch { this.app.showNotification('Copy failed','error'); }
            };
            const renderInvites = (invites, page, total, limit) => {
                if (!invList) return;
                invList.innerHTML = '';
                if (!Array.isArray(invites) || invites.length === 0) {
                    invList.innerHTML = '<div class="meta" style="opacity:.8">No invites yet</div>';
                } else {
                    invites.forEach(inv => {
                        const row = document.createElement('div');
                        row.className = 'invite-row';
                        const usesStr = inv.max_uses == null ? `${inv.uses} used (unlimited)` : `${inv.uses}/${inv.max_uses}`;
                        const expStr = inv.expires_at ? new Date(inv.expires_at).toLocaleString() : 'No expiration';
                        row.innerHTML = `
                          <div class="left">
                            <div class="code">${escapeHTML(String(inv.code))}</div>
                            <div class="invite-meta">Uses: ${escapeHTML(String(usesStr))} • Expires: ${escapeHTML(String(expStr))}</div>
                          </div>
                          <div class="invite-actions">
                            <button class="nav-btn" data-act="copy">Copy link</button>
                            <button class="nav-btn" data-act="copy-code">Copy code</button>
                            <button class="nav-btn nav-btn-danger" data-act="revoke">Revoke</button>
                          </div>`;
                        row.querySelector('[data-act="copy"]').onclick = () => copyToClipboard(buildLink(inv.code));
                        row.querySelector('[data-act="copy-code"]').onclick = () => copyToClipboard(inv.code);
                        row.querySelector('[data-act="revoke"]').onclick = async () => {
                            const ok = await this.app.showConfirm('Revoke this invite?'); if (!ok) return;
                const r = await fetchWithCSRF(`/api/admin/invites/${inv.id}`, { method:'DELETE', credentials:'include' });
                            if (r.status === 204) { this.app.showNotification('Invite revoked'); await loadInvites(invPage); }
                            else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Revoke failed','error'); }
                        };
                        invList.appendChild(row);
                    });
                    const prune = document.createElement('div');
                    prune.className = 'invite-prune';
                    prune.innerHTML = '<button id="inv-prune" class="link-btn">Clear Used and Expired codes</button>';
                    invList.appendChild(prune);
                    const pruneBtn = document.getElementById('inv-prune');
                    if (pruneBtn) pruneBtn.onclick = async (e) => {
                        e.preventDefault();
                        const ok = await this.app.showConfirm('Clear all used and expired invites?');
                        if (!ok) return;
                        const r = await fetchWithCSRF('/api/admin/invites/prune', { method:'POST', credentials:'include' });
                        if (r.ok) { this.app.showNotification('Cleared'); await loadInvites(invPage); }
                        else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Failed to clear','error'); }
                    };
                }
                const totalPages = Math.max(1, Math.ceil(total/limit));
                if (invInfo) invInfo.textContent = `Page ${page} of ${totalPages} • ${total} total`;
                if (invPrev) invPrev.disabled = page <= 1;
                if (invNext) invNext.disabled = page >= totalPages;
            };
            const loadInvites = async (page=1) => {
                invPage = page;
                const r = await fetchWithCSRF(`/api/admin/invites?page=${page}&limit=${invLimit}`, { credentials:'include' });
                if (!r.ok) { this.app.showNotification('Failed to load invites','error'); return; }
                const d = await r.json().catch(()=>({invites:[],total:0}));
                renderInvites(d.invites||[], d.page||page, d.total||0, d.limit||invLimit);
            };
            if (invPrev) invPrev.onclick = () => loadInvites(Math.max(1, invPage-1));
            if (invNext) invNext.onclick = () => loadInvites(invPage+1);
            const btnCreateInvite = document.getElementById('btn-create-invite');
            if (btnCreateInvite) btnCreateInvite.onclick = async (e) => {
                e.preventDefault();
                const maxUsesVal = document.getElementById('inv-max-uses').value.trim();
                const durationVal = document.getElementById('inv-duration').value.trim();
                const body = {};
                if (maxUsesVal !== '') { body.max_uses = parseInt(maxUsesVal, 10); }
                if (durationVal !== '') { body.duration = durationVal; }
                const r = await fetchWithCSRF('/api/admin/invites', { method:'POST', headers:{ 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify(body) });
                if (r.ok || r.status === 201) {
                    const d = await r.json().catch(()=>({}));
                    this.app.showNotification('Invite created');
                    if (d.link) { try { await navigator.clipboard.writeText(d.link); this.app.showNotification('Link copied'); } catch {} }
                    await loadInvites(1);
                } else {
                    const e = await r.json().catch(()=>({}));
                    this.app.showNotification(e.error||'Create failed','error');
                }
            };
            await loadInvites(1);

            const saveBtnTop = document.getElementById('btn-save-site-top');
            if (saveBtnTop) saveBtnTop.onclick = doSave;
            const saveBtn = document.getElementById('btn-save-site');
            if (saveBtn) saveBtn.onclick = doSave;
            const saveCore = document.getElementById('btn-save-site-core');
            if (saveCore) saveCore.onclick = doSave;

            const btnTest = document.getElementById('btn-smtp-test');
            if (btnTest) btnTest.onclick = async () => {
                const to = (document.getElementById('smtp-test-to').value||'').trim();
                if(!to){ this.app.showNotification('Enter recipient','error'); return;}
                const r = await fetchWithCSRF('/api/admin/site/test-smtp', { method:'POST', headers:{ 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ to }) });
                if (r.status===204) this.app.showNotification('Test email sent');
                else {
                    const e = await r.json().catch(()=>({}));
                    const msg = e.details ? `${e.error||'Send failed'}: ${e.details}` : (e.error||'Send failed');
                    this.app.showNotification(msg,'error');
                }
            };

            const btnTestStorage = document.getElementById('btn-test-storage');
            if (btnTestStorage) btnTestStorage.onclick = async () => {
                const r = await fetchWithCSRF('/api/admin/site/test-storage', { method:'POST', credentials:'include' });
                const statusEl = document.getElementById('storage-status');
                if (r.ok) {
                    const d = await r.json().catch(()=>({}));
                    if (statusEl) statusEl.textContent = `Current: ${d.provider||'local'} • OK`;
                    this.app.showNotification('Storage verified');
                } else {
                    const e = await r.json().catch(()=>({}));
                    if (statusEl) statusEl.textContent = `Current: ${document.getElementById('storage-provider').value} • Error`;
                    this.app.showNotification(e.error||'Storage verification failed','error');
                }
            };

            const exportBtn = document.getElementById('btn-export-upload');
            if (exportBtn) exportBtn.onclick = () => this.app.showMigrationModal();
            const saveStorageBtn = document.getElementById('btn-save-storage');
            if (saveStorageBtn) saveStorageBtn.onclick = doSave;
        }

        const isAdminLocal = isAdmin;
        const searchInput = document.getElementById('user-search');
        const results = document.getElementById('user-results');
        let timer;
        const renderRows = (users=[]) => {
            results.innerHTML = '';
            if (!users || users.length === 0) {
                const empty = document.createElement('div');
                empty.className = 'meta';
                empty.style.opacity = '.8';
                empty.textContent = 'No users found';
                results.appendChild(empty);
                return;
            }
            users.forEach(u => {
                const row = document.createElement('div');
                row.className = 'user-row';
                const left = document.createElement('div'); left.className = 'left';
                left.innerHTML = `<div class="handle">@${escapeHTML(String(u.username))}</div><div class="id">${escapeHTML(String(u.id))}</div>`;
                const right = document.createElement('div'); right.className='actions';
                const modBtn = document.createElement('button'); modBtn.className='nav-btn'; modBtn.textContent = u.is_moderator ? 'Unmod' : 'Make mod';
                modBtn.onclick = async () => { const r = await fetchWithCSRF(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ is_moderator: !u.is_moderator }) }); if (r.ok) { u.is_moderator = !u.is_moderator; modBtn.textContent = u.is_moderator ? 'Unmod' : 'Make mod'; } };
                right.appendChild(modBtn);
                if (isAdminLocal) {
                    const adminBtn = document.createElement('button'); adminBtn.className='nav-btn'; adminBtn.textContent = u.is_admin ? 'Revoke admin' : 'Make admin';
                    adminBtn.onclick = async () => { const r = await fetchWithCSRF(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ is_admin: !u.is_admin }) }); if (r.ok) { u.is_admin = !u.is_admin; adminBtn.textContent = u.is_admin ? 'Revoke admin' : 'Make admin'; } };
                    right.appendChild(adminBtn);
                    const disableBtn = document.createElement('button'); disableBtn.className='nav-btn'; disableBtn.textContent = u.is_disabled ? 'Enable' : 'Disable';
                    disableBtn.onclick = async () => { const r = await fetchWithCSRF(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ is_disabled: !u.is_disabled }) }); if (r.ok) { u.is_disabled = !u.is_disabled; disableBtn.textContent = u.is_disabled ? 'Enable' : 'Disable'; } };
                    right.appendChild(disableBtn);
                    const verifyBtn = document.createElement('button'); verifyBtn.className='nav-btn'; verifyBtn.textContent='Send verify';
                    verifyBtn.onclick = async () => { const r = await fetchWithCSRF(`/api/admin/users/${u.id}/send-verification`, { method:'POST', credentials:'include' }); if (r.status===204) this.app.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Failed','error'); } };
                    right.appendChild(verifyBtn);
                    const delBtn = document.createElement('button'); delBtn.className='nav-btn'; delBtn.style.background='var(--color-danger)'; delBtn.style.color='#fff'; delBtn.textContent='Delete';
                    delBtn.onclick = async () => { const ok = await this.app.showConfirm('Delete user?'); if (!ok) return; const r = await fetchWithCSRF(`/api/admin/users/${u.id}`, { method:'DELETE', credentials:'include' }); if (r.status===204) { row.remove(); } else { this.app.showNotification('Delete failed','error'); } };
                    right.appendChild(delBtn);
                }
                row.appendChild(left); row.appendChild(right); results.appendChild(row);
            });
        };
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
            const r = await fetchWithCSRF(`/api/admin/users?q=${encodeURIComponent(q)}&page=${page}&limit=${pageSize}`, { credentials: 'include' });
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
}
