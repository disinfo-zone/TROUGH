import { fetchWithCSRF } from '../services/api.js';
import { sanitizeAndRenderMarkdown, escapeHTML } from '../utils.js';


export default class ProfileView {
    constructor(app) {
        this.app = app;
    }

    async render(username, opts = {}) {
        this.app.beginRender('profile');
        this.app.profileUsername = username;
        this.app.profileTab = String(opts.defaultTab || 'posts');
        const suppressInfinite = !!opts.suppressInfinite;
        // Ensure gallery uses multi-column layout (remove single-column mode from image page)
        this.app.galleryEl.classList.remove('settings-mode');
        if (this.app.profileTop) this.app.profileTop.innerHTML = '';
        this.app.galleryEl.innerHTML = '';
        // Enable managed masonry after clearing content
        this.app.gallery.enableManagedMasonry();
        // Ensure we start at page 1 for profile images when rendering fresh
        this.app.page = 1;
        this.app.hasMore = true;

        // Ensure MagneticScroll is enabled for profile pages
        if (this.app.magneticScroll && this.app.magneticScroll.updateEnabledState) {
            this.app.magneticScroll.updateEnabledState();
        }

        let user = null; let imgs = { images: [] };
        try {
            const [u, i] = await Promise.all([
                fetchWithCSRF(`/api/users/${encodeURIComponent(username)}`),
                fetchWithCSRF(`/api/users/${encodeURIComponent(username)}/images?page=1`)
            ]);
            if (!u.ok) throw new Error('User not found');
            user = await u.json();
            imgs = i.ok ? await i.json() : { images: [] };
        } catch (e) {
            // Styled in-app error view
            this.app.gallery.innerHTML = '';
            if (this.app.profileTop) this.app.profileTop.innerHTML = '';
            const wrap = document.createElement('section');
            wrap.className = 'mono-col';
            wrap.style.cssText = 'margin:120px auto 0;max-width:720px;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--surface-elevated);color:var(--text-primary)';
            wrap.innerHTML = `
              <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
                <div style="font-weight:800;letter-spacing:-0.02em">User not found</div>
                <span style="opacity:.6;font-family:var(--font-mono);font-size:12px">error: profile_missing</span>
              </div>
              <div style="color:var(--text-secondary);font-family:var(--font-mono);line-height:1.6">The profile <strong>@${escapeHTML(String(username))}</strong> does not exist.</div>
              <div style="margin-top:12px"><a href="/" class="nav-btn" style="text-decoration:none">Back to river</a></div>
            `;
            this.app.galleryEl.appendChild(wrap);
            return;
        }
        const isOwner = this.app.currentUser && (this.app.currentUser.username === user.username);
        const isAdmin = !!this.app.currentUser?.is_admin;
        const isModerator = !!this.app.currentUser?.is_moderator;

        // Normalize image objects (ensure caption field exists as string)
        imgs.images = (imgs.images || []).map(img => ({
            ...img,
            caption: typeof img.caption === 'string' ? img.caption : (img.caption || '')
        }));

        // Header (no backdrop)
        const header = document.createElement('section');
        header.className = 'mono-col';
        header.style.cssText = 'padding:16px 0;color:var(--text-primary);display:flex;align-items:center;justify-content:space-between;gap:12px;margin:0 auto;position:relative';
        const safeAvatar = user.avatar_url ? String(user.avatar_url) : '';
        const avatar = `<div class="avatar-preview" style="flex:0 0 auto;"></div>`;
        const adminBtn = (isOwner && (isAdmin || isModerator)) ? '<button id="profile-admin" class="link-btn">Admin</button>' : '';
        header.innerHTML = `
          <div class="profile-left" style="display:flex;gap:12px;align-items:center;min-width:0;flex:1">
            ${avatar}
            <div class="profile-username" style="font-weight:700;font-size:1.1rem;font-family:var(--font-mono);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%">@${escapeHTML(String(user.username))}</div>
          </div>
          ${isOwner ? `
          <div class="profile-actions" style="display:flex;gap:8px;align-items:center;flex-shrink:0">
            <div class="profile-actions-inline" style="display:flex;gap:8px;align-items:center">
              <button id="profile-logout" class="nav-btn">Sign out</button>
              ${adminBtn}
              <button id="profile-settings" class="link-btn">Settings</button>
            </div>
            <div class="profile-actions-menu" style="display:none;position:relative">
              <button id="profile-actions-toggle" class="nav-btn">Options</button>
               <div id="profile-actions-panel" class="profile-menu" style="display:none;position:absolute;right:0;top:calc(100% + 8px);min-width:180px;background:var(--surface-elevated);border:1px solid var(--border);border-radius:10px;padding:6px;box-shadow:var(--shadow-2xl);z-index:2500">
                <button id="menu-settings" class="profile-item link-btn" style="display:block;width:100%;text-align:left;padding:8px 10px">Settings</button>
                ${(isAdmin || isModerator) ? '<button id="menu-admin" class="profile-item link-btn" style="display:block;width:100%;text-align:left;padding:8px 10px">Admin</button>' : ''}
                <button id="menu-signout" class="profile-item link-btn" style="display:block;width:100%;text-align:left;padding:8px 10px;color:#ff6666">Sign out</button>
              </div>
            </div>
          </div>` : ''}
        `;
        // Set avatar background via style API to avoid inline URL injection
        const avatarEl = header.querySelector('.avatar-preview');
        if (avatarEl && safeAvatar) {
            try { avatarEl.style.backgroundImage = `url('${encodeURI(safeAvatar)}')`; } catch {}
        }
        this.app.profileTop.appendChild(header);
        // If owner and unverified, show banner with resend action
        if (isOwner && this.app.currentUser && this.app.currentUser.email_verified === false) {
            const banner = document.createElement('section');
            banner.className = 'mono-col verify-banner';
            banner.innerHTML = `
              <div class="vb-body">
                <div class="vb-icon" aria-hidden="true">
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
                    <path d="M22 12a10 10 0 1 1-10-10 10 10 0 0 1 10 10z"></path>
                    <path d="M12 7v6"></path>
                    <circle cx="12" cy="17" r="1"></circle>
                  </svg>
                </div>
                <div class="vb-text">
                  <div class="vb-title">Email not verified — uploads locked</div>
                  <div class="vb-subtitle">Verify your email to upload. You can still collect images.</div>
                </div>
              </div>
              <div class="vb-actions">
                <button id="banner-resend" class="nav-btn">Resend verification</button>
              </div>`;
            this.app.profileTop.appendChild(banner);
            const btn = banner.querySelector('#banner-resend');
            if (btn) btn.onclick = async () => {
                try {
                    const r = await fetchWithCSRF('/api/me/resend-verification', { method:'POST', credentials:'include' });
                    if (r.status === 204) this.app.showNotification('Verification sent');
                    else { const e = await r.json().catch(()=>({})); this.app.showNotification(e.error||'Unable to send','error'); }
                } catch {}
            };
        }
        if (isOwner) {
            const sBtn = document.getElementById('profile-settings'); if (sBtn) sBtn.onclick = () => { try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.app.persistListState(); } catch {} history.pushState({}, '', '/settings'); this.app.settingsView.render(); };
            const aBtn = document.getElementById('profile-admin'); if (aBtn) aBtn.onclick = () => { try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.app.persistListState(); } catch {} history.pushState({}, '', '/admin'); this.app.adminView.render(); };
            const logoutBtn = document.getElementById('profile-logout'); if (logoutBtn) logoutBtn.onclick = async () => { await this.app.auth.signOut(); window.location.href = '/'; };
            // Mobile menu wiring
            const toggle = document.getElementById('profile-actions-toggle');
            const panel = document.getElementById('profile-actions-panel');
            const inline = header.querySelector('.profile-actions-inline');
            const menuWrap = header.querySelector('.profile-actions-menu');
            const applyLayout = () => {
                if (!inline || !menuWrap) return;
                // Prefer showing inline actions; if screen narrow or header overflows, switch to menu
                const narrow = window.innerWidth < 640;
                const overflow = header.scrollWidth > header.clientWidth + 1;
                if (narrow || overflow) {
                    inline.style.display = 'none';
                    menuWrap.style.display = 'block';
                } else {
                    inline.style.display = 'flex';
                    menuWrap.style.display = 'none';
                    if (panel) panel.style.display = 'none';
                }
            };
            // Initial layout and responsive handler (replace previous handler if any)
            applyLayout();
            if (this.app._profileResizeHandler) {
                window.removeEventListener('resize', this.app._profileResizeHandler);
            }
            this.app._profileResizeHandler = () => applyLayout();
            window.addEventListener('resize', this.app._profileResizeHandler);
            // Render the menu as a fixed-position overlay anchored to the toggle so it appears above all content
            let panelOpen = false;
            let cleanupFns = [];
            const originalParent = menuWrap;
            const positionPanel = () => {
                const rect = toggle.getBoundingClientRect();
                panel.style.position = 'fixed';
                panel.style.minWidth = '180px';
                panel.style.top = `${Math.round(rect.bottom + 8)}px`;
                panel.style.right = `${Math.max(8, Math.round(window.innerWidth - rect.right))}px`;
                panel.style.left = 'auto';
                panel.style.zIndex = '5000';
                panel.style.display = 'block';
            };
            const openPanel = () => {
                if (panelOpen) return;
                panelOpen = true;
                // Move to body to escape any stacking/overflow contexts
                document.body.appendChild(panel);
                positionPanel();
                const onDocClick = (ev) => { if (!panel.contains(ev.target) && ev.target !== toggle) closePanel(); };
                const onKey = (ev) => { if (ev.key === 'Escape') closePanel(); };
                const onScroll = () => positionPanel();
                const onResize = () => positionPanel();
                document.addEventListener('click', onDocClick, true);
                document.addEventListener('keydown', onKey);
                window.addEventListener('scroll', onScroll, true);
                window.addEventListener('resize', onResize);
                cleanupFns.push(() => document.removeEventListener('click', onDocClick, true));
                cleanupFns.push(() => document.removeEventListener('keydown', onKey));
                cleanupFns.push(() => window.removeEventListener('scroll', onScroll, true));
                cleanupFns.push(() => window.removeEventListener('resize', onResize));
            };
            const closePanel = () => {
                if (!panelOpen) return;
                panelOpen = false;
                try { originalParent.appendChild(panel); } catch {}
                panel.style.display = 'none';
                // run and clear listeners
                cleanupFns.forEach(fn => { try { fn(); } catch {} });
                cleanupFns = [];
            };
            if (toggle && panel) {
                toggle.onclick = (e) => { e.stopPropagation(); if (panelOpen) closePanel(); else openPanel(); };
            }
            const mSettings = document.getElementById('menu-settings'); if (mSettings) mSettings.onclick = () => { closePanel(); try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.app.persistListState(); } catch {} history.pushState({}, '', '/settings'); this.app.settingsView.render(); };
            const mAdmin = document.getElementById('menu-admin'); if (mAdmin) mAdmin.onclick = () => { closePanel(); try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.app.persistListState(); } catch {} history.pushState({}, '', '/admin'); this.app.adminView.render(); };
            const mSign = document.getElementById('menu-signout'); if (mSign) mSign.onclick = async () => { closePanel(); await this.app.auth.signOut(); window.location.href = '/'; };
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
            this.app.profileTop.appendChild(uploadPanel);
            // handlers as before
            const drop = uploadPanel.querySelector('#profile-drop');
            const fileBtn = uploadPanel.querySelector('#profile-file');
            const pick = document.createElement('input'); pick.type = 'file'; pick.accept = 'image/*'; pick.multiple = true; pick.style.display = 'none'; uploadPanel.appendChild(pick);
            const handleFiles = async (files) => {
                for (const f of files) {
                    const uploaded = await this.app.uploadImage(f, {});
                    if (uploaded) {
                        this.app.openEditModal({ id: uploaded.id, original_name: uploaded.original_name, caption: uploaded.caption || '', is_nsfw: false, filename: uploaded.filename }, null);
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
            <div id="bio-view" class="user-bio" style="flex:1">${bioText ? sanitizeAndRenderMarkdown(bioText) : ''}</div>
            ${editBtn}
          </div>
        `;
        this.app.profileTop.appendChild(bio);
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
                area.querySelector('#bio-cancel').onclick = () => { this.render(username); };
                area.querySelector('#bio-save').onclick = async () => {
            const resp = await fetchWithCSRF('/api/me/profile', { method:'PATCH', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify({ bio: input.value.slice(0,500) }) });
                    if (resp.ok) { this.app.showNotification('Bio updated'); this.render(username); }
                    else { const err = await resp.json().catch(()=>({})); this.app.showNotification(err.error||'Save failed','error'); }
                };
            };
        }

        // Collections toggle row
        const tabs = document.createElement('section');
        tabs.className = 'mono-col';
        tabs.style.cssText = 'padding:0 0 8px;margin:0 auto;';
        tabs.innerHTML = `
          <div class="tab-group" style="margin-bottom:8px">
            <button id="tab-posts" class="tab-btn" aria-pressed="true">User Images</button>
            <button id="tab-collections" class="tab-btn" aria-pressed="false">Collected</button>
          </div>`;
        this.app.profileTop.appendChild(tabs);

        const loadPosts = async () => {
            this.app.profileTab = 'posts';
            this.app.galleryEl.innerHTML = '';
            this.app.gallery.enableManagedMasonry();
            // Reset chunking state
            this.app.unrendered = [];
            this.app.page = 1;
            this.app.hasMore = true;
            // Use pre-fetched page 1 results
            const firstPage = imgs.images || [];
            if (firstPage.length > 0) {
                this.app.gallery.enqueueUnrendered(firstPage);
                this.app.gallery.maybeRevealCards();
                this.app.page = 2;
            } else {
                this.app.hasMore = false;
            }
            // Enable infinite scroll sentinel for profiles as well
            if (!suppressInfinite) this.app.setupInfiniteScroll();
        };

        const loadCollections = async () => {
            this.app.profileTab = 'collections';
            this.app.galleryEl.innerHTML = '';
            this.app.gallery.enableManagedMasonry();
            // Reset chunking state
            this.app.unrendered = [];
            this.app.page = 1;
            this.app.hasMore = true;
            try {
                const resp = await fetchWithCSRF(`/api/users/${encodeURIComponent(username)}/collections?page=1`);
                if (!resp.ok) { this.app.showNotification('Failed to load collections','error'); return; }
                const data = await resp.json();
                const firstPage = data.images || [];
                if (firstPage.length > 0) {
                    this.app.gallery.enqueueUnrendered(firstPage);
                    this.app.gallery.maybeRevealCards();
                    this.app.page = 2;
                } else {
                    this.app.hasMore = false;
                }
                // Enable infinite scroll sentinel for profiles as well
                if (!suppressInfinite) this.app.setupInfiniteScroll();
            } catch {}
        };

        const postsBtn = tabs.querySelector('#tab-posts');
        const colBtn = tabs.querySelector('#tab-collections');
        if (postsBtn && colBtn) {
            postsBtn.onclick = async () => {
                postsBtn.setAttribute('aria-pressed','true');
                colBtn.setAttribute('aria-pressed','false');
                await loadPosts();
                try { this.app.persistListState(); } catch {}
            };
            colBtn.onclick = async () => {
                postsBtn.setAttribute('aria-pressed','false');
                colBtn.setAttribute('aria-pressed','true');
                await loadCollections();
                try { this.app.persistListState(); } catch {}
            };
        }

        // Default to posts or collections based on opts; fallback to posts if available
        if (opts && opts.defaultTab === 'collections') {
            postsBtn.setAttribute('aria-pressed','false');
            colBtn.setAttribute('aria-pressed','true');
            await this.app.seedMyCollectedSet();
            await loadCollections();
        } else if ((imgs.images || []).length === 0) {
            postsBtn.setAttribute('aria-pressed','false');
            colBtn.setAttribute('aria-pressed','true');
            await loadCollections();
        } else {
            // Refresh my collected set so collect buttons reflect persisted state
            await this.app.seedMyCollectedSet();
            await loadPosts();
        }

        // Update document title and social meta for profile pages
        try {
            const siteTitle = document.querySelector('.logo')?.getAttribute('data-text') || 'TROUGH';
            document.title = `@${String(user.username)} - ${siteTitle}`;
            const ensureMeta = (name) => {
                let m = document.querySelector(`meta[name="${name}"]`);
                if (!m) { m = document.createElement('meta'); m.setAttribute('name', name); document.head.appendChild(m); }
                return m;
            };
            const ensureProp = (prop) => {
                let m = document.querySelector(`meta[property="${prop}"]`);
                if (!m) { m = document.createElement('meta'); m.setAttribute('property', prop); document.head.appendChild(m); }
                return m;
            };
            // Plain-text description from bio (strip simple markdown), fallback to site description
            const stripMD = (s) => String(s||'')
                .replace(/\!\[[^\]]*\]\([^)]*\)/g,'')
                .replace(/\[[^\]]*\]\([^)]*\)/g,'$1')
                .replace(/[\*_`>#~]/g,'')
                .replace(/\s+/g,' ')
                .trim();
            let desc = stripMD(user.bio || '');
            if (!desc) {
                try { const ss = JSON.parse(localStorage.getItem('site_settings')||'null'); desc = String(ss?.seo_description||''); } catch {}
            }
            if (desc.length > 280) desc = desc.slice(0, 280);
            if (desc) ensureMeta('description').setAttribute('content', desc);
            // Image: latest user image, else site social image
            let imgAbs = '';
            const first = (imgs.images||[])[0];
            if (first && first.filename) {
                const fn = String(first.filename);
                imgAbs = (/^https?:\/\//i.test(fn)) ? fn : (location.origin + '/uploads/' + fn);
            } else {
                try { const ss = JSON.parse(localStorage.getItem('site_settings')||'null'); const si = String(ss?.social_image_url||''); if (si) { imgAbs = si.startsWith('http') ? si : (si.startsWith('/') ? (location.origin + si) : si); } } catch {}
            }
            const ogType = 'profile';
            ensureProp('og:site_name').setAttribute('content', siteTitle);
            ensureProp('og:title').setAttribute('content', `@${String(user.username)} - ${siteTitle}`);
            if (desc) ensureProp('og:description').setAttribute('content', desc);
            ensureProp('og:type').setAttribute('content', ogType);
            ensureProp('og:url').setAttribute('content', location.href);
            if (imgAbs) {
                ensureProp('og:image').setAttribute('content', imgAbs);
                ensureProp('og:image:alt').setAttribute('content', `@${String(user.username)} profile image`);
            }
            const twCard = imgAbs ? 'summary_large_image' : 'summary';
            ensureMeta('twitter:card').setAttribute('content', twCard);
            ensureMeta('twitter:title').setAttribute('content', `@${String(user.username)} - ${siteTitle}`);
            if (desc) ensureMeta('twitter:description').setAttribute('content', desc);
            if (imgAbs) {
                ensureMeta('twitter:image').setAttribute('content', imgAbs);
                ensureMeta('twitter:image:alt').setAttribute('content', `@${String(user.username)} profile image`);
            }
        } catch {}
    }
}
