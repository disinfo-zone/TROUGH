import { sanitizeAndRenderMarkdown, escapeHTML } from './app/utils.js';
import MagneticScroll from './app/MagneticScroll.js';
import { initCSRFToken, fetchWithCSRF } from './app/services/api.js';
import Auth from './app/auth.js';
import ProfileView from './app/views/ProfileView.js';
import SettingsView from './app/views/SettingsView.js';
import AdminView from './app/views/AdminView.js';
import Router from './app/router.js';
import Gallery from './app/gallery.js';

// PREMIUM GALLERY APPLICATION
class TroughApp {
    constructor() {
        // Original properties
        this.page = 1;
        this.loading = false;
        this.hasMore = true;
        this.currentUser = null;
        // (legacy magnetic scroll state removed)
        this._lastScrollSaveTs = 0;
        this.isRestoring = false;
        // Rendering control to avoid stale inserts across route changes
        this.renderEpoch = 0;
        this.pendingTimers = new Set();
        this.routeMode = 'home';
        // Interaction flag for conservative prefill before any user action
        this._userInteracted = false;
		// Track my collected image ids for UI state
		this._myCollectedSet = new Set();
        // Profile state for chunked loading
        this.profileUsername = null;
        this.profileTab = 'posts';
        
        // DOM elements
        const galleryEl = document.getElementById('gallery');
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
            if (galleryEl && galleryEl.parentNode) {
                galleryEl.parentNode.insertBefore(this.profileTop, galleryEl);
            }
        }

        // Initialize drift-based MagneticScroll once for the whole app lifecycle
        this.magneticScroll = new MagneticScroll({
            minCardIndex: 0,
            attractionStrength: 0.012,
            damping: 0.94,
            maxDriftSpeed: 0.6,
            settleDelay: 100,
            effectiveRange: 250,
        });

        this.auth = new Auth(this);
        this.profileView = new ProfileView(this);
        this.settingsView = new SettingsView(this);
        this.adminView = new AdminView(this);
        this.router = new Router();
        this.gallery = new Gallery(this);
        
        this.init();
    }
    
    async seedMyCollectedSet() {
        try {
            // Only for logged-in users
            if (!this.currentUser || !this.currentUser.username) { this._myCollectedSet = new Set(); return; }
            // Fetch first page of my collections to initialize state
            const resp = await fetchWithCSRF(`/api/users/${encodeURIComponent(this.currentUser.username)}/collections?page=1`, { credentials: 'include' });
            if (!resp.ok) { this._myCollectedSet = this._myCollectedSet || new Set(); return; }
            const data = await resp.json();
            this._myCollectedSet = new Set((data.images || []).map(img => String(img.id)));
        } catch {
            this._myCollectedSet = this._myCollectedSet || new Set();
        }
    }

    async init() {
        // Initialize logo/site name from cache first to avoid flash
        try {
            const cached = localStorage.getItem('site_settings');
            if (cached) {
                const s = JSON.parse(cached);
                if (s && s.site_name) {
                    const logo = document.querySelector('.logo');
                    if (logo) { logo.textContent = s.site_name; logo.setAttribute('data-text', s.site_name); }
                    if (location.pathname === '/') {
                        document.title = s.seo_title || `${s.site_name} · AI IMAGERY`;
                    }
                }
            }
        } catch {}
        
        // Initialize CSRF token immediately
        await initCSRFToken();
        
        await this.auth.checkAuth();
		// Seed my collection state early for correct UI on first paint
		await this.seedMyCollectedSet();
        this.setupRouter();
        this.setupEventListeners();
        this.gallery.setupImageLazyLoader();

        await this.applyPublicSiteSettings(); // Moved this line up

        this.router.resolve();
    }

    // Persist the current list page state (feed or profile) so we can restore on back
    persistListState(pathnameOverride) {
        try {
            const path = pathnameOverride || location.pathname;
            const key = `trough:list:${path}`;
            // Find first fully visible image card for restoration anchor (prefer id)
            let firstVisibleIndex = 0;
            let firstVisibleId = null;
            try {
                const cards = Array.from(document.querySelectorAll('.image-card'));
                const viewportTop = window.scrollY;
                const viewportBottom = viewportTop + window.innerHeight;
                for (let i = 0; i < cards.length; i++) {
                    const r = cards[i].getBoundingClientRect();
                    const top = r.top + viewportTop;
                    const bottom = top + r.height;
                    const visible = Math.max(0, Math.min(bottom, viewportBottom) - Math.max(top, viewportTop));
                    if (visible >= Math.min(r.height, window.innerHeight) * 0.75) { firstVisibleIndex = i; break; }
                }
                const anchor = cards[firstVisibleIndex];
                if (anchor && anchor.dataset && anchor.dataset.imageId) firstVisibleId = anchor.dataset.imageId;
            } catch {}
            const state = {
                path,
                page: this.page,
                hasMore: this.hasMore,
                scrollY: typeof window !== 'undefined' ? window.scrollY : 0,
                firstVisibleIndex,
                firstVisibleId,
                profileTab: this.profileTab || 'posts',
                savedAt: Date.now(),
            };
            console.log('[TROUGH] Persisting list state:', { key, state });
            sessionStorage.setItem(key, JSON.stringify(state));
            try {
                const merged = Object.assign({}, history.state || {}, { troughList: state });
                history.replaceState(merged, '');
            } catch {}
        } catch (e) {
            console.error('[TROUGH] Error persisting list state:', e);
        }
    }

    // Restore a list page by refetching pages up to the saved page and scrolling back
    async restoreListState(pathnameOverride) {
        const path = pathnameOverride || location.pathname;
        const key = `trough:list:${path}`;
        let state = null;
        try { state = JSON.parse(sessionStorage.getItem(key) || 'null'); } catch { state = null; }
        console.log('[TROUGH] Restoring list state:', { key, state, path });
        if (!state || typeof state.page !== 'number') {
            console.log('[TROUGH] No valid state found, using default behavior');
            // Fallback to default behavior
            if (path === '/') {
                this.gallery.clear();
                if (this.profileTop) this.profileTop.innerHTML = '';
                this.page = 1;
                this.hasMore = true;
                await this.gallery.loadImages();
            } else if (path.startsWith('/@')) {
                // Fresh render of profile page 1 when no saved page info exists
                const username = decodeURIComponent(path.slice(2));
                await this.profileView.render(username);
                try { if (typeof state?.scrollY === 'number') window.scrollTo(0, state.scrollY); } catch {}
            }
            return;
        }

        if (path === '/') {
            // Rebuild the home feed up to the saved page
            this.gallery.clear();
            if (this.profileTop) this.profileTop.innerHTML = '';
            this.page = 1;
            this.hasMore = true;
            this.isRestoring = true;
            const targetPage = Math.max(1, state.page);
            while (this.page <= targetPage && this.hasMore) {
                await this.gallery.loadImages();
                // Immediately reveal a small batch so the anchor card exists in DOM
                this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
            }
            // Prefer anchoring by id, then index; fallback to scrollY
            const prevRestore = this.isRestoring;
            this.isRestoring = true; // force synchronous reveals during anchoring
            let targetCard = null;
            if (state.firstVisibleId) {
                const selector = `.image-card[data-image-id="${CSS.escape(String(state.firstVisibleId))}"]`;
                targetCard = document.querySelector(selector);
                // If not present yet, progressively reveal/fetch until it exists or we run out
                let guard = 0;
                while (!targetCard && guard < 50) {
                    let revealed = 0;
                    if (this.gallery.unrendered && this.gallery.unrendered.length) {
                        revealed = this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    } else if (this.hasMore && !this.loading && (this.routeMode === 'home' || this.routeMode === 'profile')) {
                        await this.gallery.loadImages();
                        revealed = this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    } else {
                        break;
                    }
                    if (!revealed) break;
                    targetCard = document.querySelector(selector);
                    guard++;
                }
            }
            if (!targetCard && Number.isFinite(state.firstVisibleIndex) && state.firstVisibleIndex > 0) {
                let cards = Array.from(document.querySelectorAll('.image-card'));
                // If the indexed card doesn't exist yet, try revealing until it does or we run out
                while (!cards[state.firstVisibleIndex] && (this.gallery.unrendered && this.gallery.unrendered.length)) {
                    const revealed = this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    if (!revealed) break;
                    cards = Array.from(document.querySelectorAll('.image-card'));
                }
                const cards2 = Array.from(document.querySelectorAll('.image-card'));
                targetCard = cards2[state.firstVisibleIndex] || null;
            }
            if (targetCard) {
                const rect = targetCard.getBoundingClientRect();
                const y = rect.top + window.scrollY - (document.getElementById('nav')?.offsetHeight || 0) - 8;
                try { window.scrollTo(0, Math.max(0, y)); } catch {}
                // After anchoring, ensure there is sufficient content below to allow further scrolling
                await this.gallery.topUpBelowViewport(4);
            } else if (typeof state.scrollY === 'number') {
                try { window.scrollTo(0, state.scrollY); } catch {}
                await this.gallery.topUpBelowViewport(4);
            }
            this.isRestoring = prevRestore;
        } else if (path.startsWith('/@')) {
            // For profiles, rebuild up to the saved page and anchor like home
            const username = decodeURIComponent(path.slice(2));
            const targetPage = Math.max(1, state.page);
            const targetTab = state.profileTab || 'posts';
            // Fresh render honoring the saved tab
            this.gallery.clear();
            if (this.profileTop) this.profileTop.innerHTML = '';
            this.page = 1;
            this.hasMore = true;
            this.isRestoring = true;
            await this.profileView.render(username, { defaultTab: targetTab, suppressInfinite: true });
            // Load subsequent pages up to target
            while (this.page <= targetPage && this.hasMore) {
                await this.gallery.loadImages();
                this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
            }
            // Anchor by id or index if available
            const prevRestore2 = this.isRestoring;
            this.isRestoring = true; // force synchronous reveals during anchoring
            let targetCard = null;
            if (state.firstVisibleId) {
                const selector = `.image-card[data-image-id="${CSS.escape(String(state.firstVisibleId))}"]`;
                targetCard = document.querySelector(selector);
                // If not present yet, progressively reveal/fetch until it exists or we run out
                let guard = 0;
                while (!targetCard && guard < 50) {
                    let revealed = 0;
                    if (this.gallery.unrendered && this.gallery.unrendered.length) {
                        revealed = this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    } else if (this.hasMore && !this.loading && (this.routeMode === 'home' || this.routeMode === 'profile')) {
                        await this.gallery.loadImages();
                        revealed = this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    } else {
                        break;
                    }
                    if (!revealed) break;
                    targetCard = document.querySelector(selector);
                    guard++;
                }
            }
            if (!targetCard && Number.isFinite(state.firstVisibleIndex) && state.firstVisibleIndex > 0) {
                let cards = Array.from(document.querySelectorAll('.image-card'));
                while (!cards[state.firstVisibleIndex] && (this.gallery.unrendered && this.gallery.unrendered.length)) {
                    const revealed = this.gallery.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    if (!revealed) break;
                    cards = Array.from(document.querySelectorAll('.image-card'));
                }
                const cards2 = Array.from(document.querySelectorAll('.image-card'));
                targetCard = cards2[state.firstVisibleIndex] || null;
            }
            if (targetCard) {
                const rect = targetCard.getBoundingClientRect();
                const y = rect.top + window.scrollY - (document.getElementById('nav')?.offsetHeight || 0) - 8;
                try { window.scrollTo(0, Math.max(0, y)); } catch {}
                await this.gallery.topUpBelowViewport(4);
            } else if (typeof state.scrollY === 'number') {
                try { window.scrollTo(0, state.scrollY); } catch {}
                await this.gallery.topUpBelowViewport(4);
            }
            this.isRestoring = prevRestore2;
            // Re-enable infinite scrolling after anchoring
            this.setupInfiniteScroll();
        }
    }

    setupRouter() {
        this.router.add('/', () => this.goHome());
        this.router.add(/^\/@(\w+)$/, (username) => this.profileView.render(username));
        this.router.add('/settings', () => this.settingsView.render());
        this.router.add('/admin', () => this.adminView.render());
        this.router.add(/^\/i\/(\w+)$/, (id) => this.renderImagePage(id));
        this.router.add('/reset', () => this.renderResetPage());
        this.router.add('/verify', () => this.renderVerifyPage());
        this.router.add('/register', () => this.handleRegistrationRoute());
        this.router.add(/^\/([a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?)$/, (slug) => this.renderCMSPage(slug));

        window.onpopstate = () => this.router.resolve();
    }

    async goHome() {
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.beginRender('home');
        this.gallery.enableManagedMasonry();
        await this.gallery.loadImages();
        this.setupInfiniteScroll();
        const logo = document.querySelector('.logo');
        if (logo && !logo.getAttribute('data-text')) {
            logo.setAttribute('data-text', logo.textContent || '');
        }
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) {
            this.magneticScroll.updateEnabledState();
        }
    }

    async handleRegistrationRoute() {
        const url = new URL(location.href);
        const invite = url.searchParams.get('invite');

        this.showAuthModal();
        const tabs = document.querySelectorAll('.auth-tab');
        const registerTab = Array.from(tabs).find(t => t.dataset.tab === 'register');
        const loginTab = Array.from(tabs).find(t => t.dataset.tab === 'login');

        const proceedToRegister = async () => {
            if (registerTab) {
                tabs.forEach(t => t.classList.remove('active')); registerTab.classList.add('active');
                const loginForm = document.getElementById('login-form'); const registerForm = document.getElementById('register-form'); const submitBtn = document.getElementById('auth-submit');
                if (loginForm && registerForm && submitBtn) { loginForm.style.display='none'; registerForm.style.display='block'; submitBtn.textContent='Create Account'; }
            }
        };

        const proceedToLogin = async (message = 'Registration is currently disabled') => {
            this.showNotification(message, 'error');
            if (loginTab) {
                tabs.forEach(t => t.classList.remove('active')); loginTab.classList.add('active');
                const loginForm = document.getElementById('login-form'); const registerForm = document.getElementById('register-form'); const submitBtn = document.getElementById('auth-submit');
                if (loginForm && registerForm && submitBtn) { loginForm.style.display='block'; registerForm.style.display='none'; submitBtn.textContent='Sign In'; }
            }
        };

        if (invite) {
            try {
                const r = await fetchWithCSRF(`/api/invites/validate?code=${encodeURIComponent(invite)}`);
                if (r.status === 204) {
                    this._pendingInvite = invite;
                    await proceedToRegister();
                } else {
                    const data = await r.json().catch(() => ({}));
                    await proceedToLogin(data.error || 'Invalid invitation link');
                }
            } catch (e) {
                console.error('Invite validation error:', e);
                await proceedToLogin('Unable to validate invite. Connection error.');
            }
        } else {
            if (window.__PUBLIC_REG_ENABLED__ !== false) {
                await proceedToRegister();
            } else {
                await proceedToLogin();
            }
        }
    }

    // Begin a new render epoch and cleanup any pending async UI work
    beginRender(mode) {
        try {
            this.renderEpoch++;
            this.routeMode = mode || this.routeMode || 'home';
            // Cancel any staggered timers from previous view
            if (this.pendingTimers && this.pendingTimers.size) {
                for (const id of this.pendingTimers) { try { clearTimeout(id); } catch {} }
                this.pendingTimers.clear();
            }
            // Remove infinite scroll listener if present
            if (this._infiniteScrollCleanup) { try { this._infiniteScrollCleanup(); } catch {} this._infiniteScrollCleanup = null; }

            this.gallery.reset();

            // Stop any in-flight scroll animations
            if (this._activeScrollAnim) { this._activeScrollAnim.cancelled = true; this._activeScrollAnim = null; }
        } catch {}
    }

    trackTimeout(id) { try { if (id) this.pendingTimers.add(id); } catch {} return id; }
    untrackTimeout(id) { try { if (id) this.pendingTimers.delete(id); } catch {} }

    async applyPublicSiteSettings() {
        try {
            const r = await fetchWithCSRF('/api/site');
            if (!r.ok) return;
            const s = await r.json();
            // Cache for next load to avoid logo/name flash
            try { localStorage.setItem('site_settings', JSON.stringify(s)); } catch {}
            window.__SITE_EMAIL_ENABLED__ = !!s.email_enabled;
            window.__REQUIRE_VERIFY__ = !!s.require_email_verification;
            window.__PUBLIC_REG_ENABLED__ = s.public_registration_enabled !== false; // default true
            if (s.from_email) window.__SITE_FROM_EMAIL__ = s.from_email;
            if (s.site_name) {
                const logo = document.querySelector('.logo');
                if (logo) { logo.textContent = s.site_name; logo.setAttribute('data-text', s.site_name); }
                // Only set the site-wide title on the home route.
                if (location.pathname === '/') {
                    document.title = s.seo_title || `${s.site_name} · AI IMAGERY`;
                }
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
            // Only set the site description on the home route; profile/CMS routes manage their own.
            if (location.pathname === '/') {
                setMeta('description', s.seo_description || '');
                // Ensure OG/Twitter reflect site defaults on the home page
                this.applySiteDefaultMeta({ settings: s });
            }
        } catch {}
    }

    // Ensure OG/Twitter tags reflect site defaults (index SEO). Allows overriding the title/url.
    applySiteDefaultMeta(opts={}) {
        try {
            const s = opts.settings || JSON.parse(localStorage.getItem('site_settings')||'null') || {};
            const siteTitle = document.querySelector('.logo')?.getAttribute('data-text') || s.site_name || 'TROUGH';
            const title = String(opts.overrideTitle || document.title || s.seo_title || `${siteTitle} · AI IMAGERY`);
            const desc = String(s.seo_description || '');
            const ensureProp = (prop) => { let m = document.querySelector(`meta[property="${prop}"]`); if (!m) { m = document.createElement('meta'); m.setAttribute('property', prop); document.head.appendChild(m); } return m; };
            const ensureName = (name) => { let m = document.querySelector(`meta[name="${name}"]`); if (!m) { m = document.createElement('meta'); m.setAttribute('name', name); document.head.appendChild(m); } return m; };
            const toAbs = (u) => {
                if (!u) return '';
                if (/^https?:\/\//i.test(u)) return u;
                if (u.startsWith('/')) return location.origin + u;
                return u;
            };
            const img = toAbs(String(s.social_image_url||''));
            ensureProp('og:site_name').setAttribute('content', siteTitle);
            ensureProp('og:title').setAttribute('content', title);
            if (desc) ensureProp('og:description').setAttribute('content', desc);
            ensureProp('og:type').setAttribute('content', 'website');
            ensureProp('og:url').setAttribute('content', String(opts.overrideUrl || location.href));
            if (img) {
                ensureProp('og:image').setAttribute('content', img);
                ensureProp('og:image:alt').setAttribute('content', title);
            }
            const card = img ? 'summary_large_image' : 'summary';
            ensureName('twitter:card').setAttribute('content', card);
            ensureName('twitter:title').setAttribute('content', title);
            if (desc) ensureName('twitter:description').setAttribute('content', desc);
            if (img) {
                ensureName('twitter:image').setAttribute('content', img);
                ensureName('twitter:image:alt').setAttribute('content', title);
            }
        } catch {}
    }

    // Apply home page SEO (title + default OG/Twitter) using cached settings
    applyHomeSEO() {
        try {
            const cached = localStorage.getItem('site_settings');
            let s = null; try { s = JSON.parse(cached||'null'); } catch { s = null; }
            const siteTitle = document.querySelector('.logo')?.getAttribute('data-text') || s?.site_name || 'TROUGH';
            const title = s?.seo_title || `${siteTitle} · AI IMAGERY`;
            document.title = title;
            this.applySiteDefaultMeta({ overrideTitle: title, overrideUrl: location.href });
        } catch {}
    }


    setupEventListeners() {
        // Profile button always goes to profile
        this.authBtn.addEventListener('click', async () => {
            if (this.currentUser) {
                try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.persistListState(); } catch {}
                history.pushState({}, '', `/@${encodeURIComponent(this.currentUser.username)}`);
                await this.profileView.render(this.currentUser.username);
            } else {
                this.auth.showAuthModal();
            }
        });

        this.auth.setupAuthModal();
        this.setupUpload();
        this.setupLightbox();

        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                this.closeAuthModal();
                this.closeLightbox();
            }
        });

        // Throttled scroll position persistence for feed/profile
        window.addEventListener('scroll', () => {
            const now = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
            if (now - this._lastScrollSaveTs < 250) return;
            this._lastScrollSaveTs = now;
            if (location.pathname === '/' || location.pathname.startsWith('/@')) {
                console.log('[TROUGH] Scroll event triggering persistListState');
                this.persistListState();
            }
        }, { passive: true });

        // Persist scroll position before leaving the page (hard nav or refresh)
        window.addEventListener('pagehide', () => {
            if (location.pathname === '/' || location.pathname.startsWith('/@')) {
                console.log('[TROUGH] Pagehide event triggering persistListState');
                this.persistListState();
            }
        });

        // Intercept internal link clicks in gallery/profile areas to SPA-navigate
        const handleInternalLink = async (e) => {
            const anchor = e.target.closest('a[href]');
            if (!anchor) return;
            const href = anchor.getAttribute('href') || '';
            if (!href.startsWith('/') || href.startsWith('//')) return; // ignore external/relative
            e.preventDefault();
            e.stopPropagation();
            try { 
                if (location.pathname === '/' || location.pathname.startsWith('/@')) {
                    console.log('[TROUGH] Internal link click triggering persistListState');
                    this.persistListState(); 
                }
            } catch {}
            this.router.navigateTo(href);
        };
        if (this.gallery.gallery) this.gallery.gallery.addEventListener('click', handleInternalLink, true);
        if (this.profileTop) this.profileTop.addEventListener('click', handleInternalLink, true);
        const nav = document.getElementById('nav');
        if (nav) nav.addEventListener('click', handleInternalLink, true);

        // Intercept logo click to SPA-navigate home (fresh)
        const logo = document.querySelector('.logo');
        if (logo) {
            logo.addEventListener('click', (e) => {
                e.preventDefault();
                e.stopPropagation();
                e.stopImmediatePropagation();
                this.router.navigateTo('/');
            }, true);
        }
    }







    async openEditModal(image, cardNode) {
        let filename = image.filename;
        if (!filename && image.id) {
            try { const r = await fetchWithCSRF(`/api/images/${image.id}`); if (r.ok) { const d = await r.json(); filename = d.filename; } } catch {}
        }
        const overlay = document.createElement('div');
        overlay.style.cssText = 'position:fixed;inset:0;z-index:2700;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
        const panel = document.createElement('div');
        panel.style.cssText = 'max-width:980px;width:100%;max-height:90vh;overflow:auto;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
        panel.innerHTML = `
            ${filename ? `<div style="display:flex;justify-content:center;"><img src="${getImageURL(filename)}" alt="" style="max-height:60vh;width:auto;border-radius:10px;border:1px solid var(--border);margin-bottom:12px"/></div>` : ''}
            <div style="position:sticky;bottom:0;background:var(--surface-elevated);border-top:1px solid var(--border);padding-top:12px;display:grid;gap:12px">
              <input id="e-title" placeholder="Title" value="${escapeHTML(String(image.title || image.original_name || ''))}" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary)"/>
              <textarea id="e-caption" placeholder="Caption" rows="3" maxlength="2000" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary)">${escapeHTML(String(image.caption||''))}</textarea>
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
            const resp = await fetchWithCSRF(`/api/images/${image.id}`, { method:'PATCH', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify(body) });
            if (resp.ok) { overlay.remove(); this.showNotification('Saved'); location.reload(); } else { this.showNotification('Save failed', 'error'); }
        };
    }

    openLightbox(image) {
        const lightboxImg = document.getElementById('lightbox-img');
        const lightboxTitle = document.getElementById('lightbox-title');
        const lightboxAuthor = document.getElementById('lightbox-author');
        const lightboxCollect = document.getElementById('lightbox-collect');
        const lightboxCaption = document.getElementById('lightbox-caption');
        if (!lightboxImg) return;

        if (image.filename) {
            lightboxImg.src = getImageURL(image.filename);
            lightboxImg.alt = image.original_name || image.title || '';
        }
        const username = image.username || image.author || 'Unknown';
        const titleText = image.title || image.original_name || 'Untitled';
        // Title becomes a link to the single-image page (escape to prevent XSS)
        lightboxTitle.innerHTML = `<a href="/i/${encodeURIComponent(image.id)}" class="image-link" style="color:inherit;text-decoration:none">${escapeHTML(String(titleText))}</a>`;
        const link = lightboxTitle.querySelector('a.image-link');
        if (link) {
            link.addEventListener('click', async (e) => {
                e.preventDefault();
                try { 
                    if (location.pathname === '/' || location.pathname.startsWith('/@')) {
                        console.log('[TROUGH] Lightbox link click triggering persistListState');
                        this.persistListState(); 
                    }
                } catch {}
                history.pushState({}, '', link.getAttribute('href'));
                this.closeLightbox();
                await this.renderImagePage(image.id);
            });
        }
        lightboxAuthor.innerHTML = `<a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${escapeHTML(String(username))}</a>`;
        lightboxAuthor.style.fontFamily = 'var(--font-mono)';
        if (lightboxCollect) {
            lightboxCollect.classList.remove('collected');
            lightboxCollect.classList.add('collect-btn');
            lightboxCollect.textContent = '✧';
            // Hide for owner
            if (this.currentUser && image && image.username && this.currentUser.username === image.username) {
                lightboxCollect.style.display = 'none';
            } else {
                lightboxCollect.style.display = '';
                // Initially reflect collected status based on cached state
                const cached = this._myCollectedSet || new Set();
                if (cached.has(String(image.id))) {
                    lightboxCollect.classList.add('collected');
                    lightboxCollect.textContent = '✦';
                } else {
                    lightboxCollect.classList.remove('collected');
                    lightboxCollect.textContent = '✧';
                }
                lightboxCollect.onclick = async () => {
                    await this.toggleCollect(image.id, lightboxCollect);
                    // Update cache
                    if (!this._myCollectedSet) this._myCollectedSet = new Set();
                    if (lightboxCollect.classList.contains('collected')) this._myCollectedSet.add(String(image.id));
                    else this._myCollectedSet.delete(String(image.id));
                };
            }
        }
        // Render caption (sanitized markdown) in lightbox
        if (lightboxCaption) {
            lightboxCaption.innerHTML = image.caption ? sanitizeAndRenderMarkdown(String(image.caption)) : '';
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
        // Wire close actions programmatically to comply with CSP (no inline handlers)
        const backdrop = document.querySelector('#lightbox .lightbox-backdrop');
        const closeBtn = document.querySelector('#lightbox .lightbox-close');
        if (backdrop) backdrop.addEventListener('click', () => this.closeLightbox());
        if (closeBtn) closeBtn.addEventListener('click', () => this.closeLightbox());
    }

    

    async toggleCollect(imageId, btnEl=null) {
        if (!this.currentUser) { this.showAuthModal(); return; }
        const btn = btnEl || document.getElementById('lightbox-collect');
        if (!btn) return;
        const was = btn.classList.contains('collected');
        btn.classList.toggle('collected');
        btn.textContent = btn.classList.contains('collected') ? '✦' : '✧';
        try {
            const response = await fetchWithCSRF(`/api/images/${imageId}/collect`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include' });
            if (!response.ok) {
                btn.classList.toggle('collected');
                btn.textContent = btn.classList.contains('collected') ? '✦' : '✧';
                if (response.status === 401) { localStorage.removeItem('token'); localStorage.removeItem('user'); this.currentUser = null; await this.checkAuth(); this.showAuthModal(); }
                else { this.showNotification('Collect failed', 'error'); }
            }
            // Update my in-memory collected set for persistence across routes
            if (btn.classList.contains('collected')) this._myCollectedSet.add(String(imageId)); else this._myCollectedSet.delete(String(imageId));
        } catch { btn.classList.toggle('collected'); btn.textContent = btn.classList.contains('collected') ? '✦' : '✧'; }
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
        
        // Create file input for click-to-upload functionality
        const fileInput = document.createElement('input');
        fileInput.type = 'file';
        fileInput.accept = 'image/*';
        fileInput.multiple = true;
        fileInput.style.display = 'none';
        document.body.appendChild(fileInput);
        
        const handleFileSelect = async (e) => {
            const files = Array.from(e.target.files || []);
            for (const file of files) {
                const uploaded = await this.uploadImage(file, {});
                if (uploaded) {
                    this.openEditModal({ id: uploaded.id, original_name: uploaded.original_name, caption: uploaded.caption || '', is_nsfw: false, filename: uploaded.filename }, null);
                }
            }
            // Clear the input to allow selecting the same file again
            e.target.value = '';
        };
        
        // Add click handler to upload zone
        this.uploadZone.addEventListener('click', (e) => {
            // Only trigger if clicking on the upload content, not the backdrop
            if (e.target.closest('.upload-content')) {
                fileInput.click();
            }
        });
        
        fileInput.addEventListener('change', handleFileSelect);
        
        document.addEventListener('dragenter', handleDragEnter);
        document.addEventListener('dragleave', handleDragLeave);
        document.addEventListener('dragover', handleDragOver);
        document.addEventListener('drop', handleDrop);
        // Wire backdrop close for upload zone as defensive UX
        const uzBackdrop = document.querySelector('#upload-zone .upload-backdrop');
        if (uzBackdrop) uzBackdrop.addEventListener('click', () => this.uploadZone.classList.remove('active'));
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
        
        this.gallery.showLoader();
        
        try {
            const response = await fetchWithCSRF('/api/upload', { method: 'POST', credentials: 'include', body: formData });
            
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
                this.auth.checkAuth();
                this.auth.showAuthModal();
            } else if (response.status === 403) {
                const error = await response.json().catch(() => ({}));
                const msg = error.error || 'Uploads are disabled until you verify your email.';
                await this.showErrorModal('Email verification required', msg + '\n\nUse Settings → Resend verification to get a new link.');
            } else {
                const error = await response.json().catch(() => ({}));
                await this.showErrorModal('Upload failed', error.error || 'Unknown error');
            }
        } catch (error) {
            await this.showErrorModal('Upload failed', (error && error.message) || 'Network error');
        } finally {
            this.gallery.hideLoader();
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
                  <div style="font-weight:700">${escapeHTML(String(title||'Error'))}</div>
                </div>
                <div style="color:var(--text-secondary);font-family:var(--font-mono);white-space:pre-wrap;margin-bottom:12px">${escapeHTML(String(message||''))}</div>
                <div style="color:var(--text-tertiary);font-size:12px;line-height:1.5;margin:-4px 0 12px">
                  We’re actively tuning our filters. If this seems wrong, please email details to ${escapeHTML(window.__SITE_FROM_EMAIL__||'our support email')}.
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

    setupInfiniteScroll() {
        // Ensure only one scroll listener is attached at a time
        if (this._infiniteScrollCleanup) { try { this._infiniteScrollCleanup(); } catch {} this._infiniteScrollCleanup = null; }
        // Prefer an IntersectionObserver sentinel to avoid premature loads on mobile
        const sentinel = document.createElement('div');
        sentinel.id = 'infinite-scroll-sentinel';
        sentinel.style.cssText = 'width:1px;height:1px;';
        // Ensure sentinel is appended at the end of the gallery (after any unrendered reveals too)
        const appendSentinel = () => {
            try {
                if (!this.gallery.gallery) return;
                if (!document.getElementById('infinite-scroll-sentinel')) {
                    this.gallery.gallery.appendChild(sentinel);
                }
            } catch {}
        };
        appendSentinel();

        const isMobile = window.innerWidth <= 600;
        const margin = isMobile ? '50px 0px 50px 0px' : '200px 0px 200px 0px';
        let io = null;
		// State to avoid repeated fetches while sentinel remains intersecting
		this._infinitePendingExit = false;
        this._autoFillDone = false;
        this._userInteracted = false;
        const markInteracted = () => { this._userInteracted = true; };
        window.addEventListener('touchstart', markInteracted, { passive: true, once: true });
        window.addEventListener('wheel', markInteracted, { passive: true, once: true });
        window.addEventListener('keydown', markInteracted, { passive: true, once: true });

		try {
			const pushSentinelOut = () => {
				let cycles = 0;
				const step = () => {
					if (cycles >= 8) return; // safety cap per intersection
					const maxReveal = (window.innerWidth <= 600) ? 2 : 4;
					const revealed = this.gallery.maybeRevealCards(maxReveal);
					cycles++;
					// If sentinel still visible and we revealed something, keep pushing
					const rect = sentinel.getBoundingClientRect();
					const stillIntersecting = rect.top < (window.innerHeight + 1);
					if (revealed > 0 && stillIntersecting) {
						requestAnimationFrame(step);
					}
				};
				requestAnimationFrame(step);
			};

			io = new IntersectionObserver((entries) => {
				for (const e of entries) {
					if (e.target !== sentinel) continue;
					if (!e.isIntersecting) {
						this._infinitePendingExit = false;
						continue;
					}

					// Always try to reveal queued cards; push sentinel out of view in small steps
					pushSentinelOut();

					// If nothing left to reveal, consider fetching more
					if (!this.hasMore) continue;
					if (this._infinitePendingExit) continue;
					if (this.loading) continue;

					// Before user interaction: only auto-fill once if viewport isn't filled yet
					// After user interaction: allow normal infinite scrolling behavior
					const doc = document.documentElement;
					const needsFill = (doc.scrollHeight <= (window.innerHeight + 200));
					if (!this._userInteracted) {
						// Before first interaction: be conservative about auto-filling
						if (this._autoFillDone || !needsFill) continue;
						this._autoFillDone = true;
					} else {
						// After first interaction: allow normal infinite scrolling
						// Only prevent loading if we don't need more content and have nothing queued
						if (!needsFill && this.gallery.unrendered.length === 0) continue;
					}

					this._infinitePendingExit = true;
					this.gallery.loadImages();
				}
			}, { root: null, rootMargin: margin, threshold: 0 });
		} catch {}

        if (io) {
            io.observe(sentinel);
            this._infiniteScrollCleanup = () => { try { io.disconnect(); } catch {}; try { sentinel.remove(); } catch {}; };
        } else {
            // Fallback to scroll handler if IO unavailable
            let ticking = false;
            const handleScroll = () => {
                if (!ticking) {
                    requestAnimationFrame(() => {
                        const { scrollTop, scrollHeight, clientHeight } = document.documentElement;
                        if (scrollTop + clientHeight >= scrollHeight - 800) {
                            this.gallery.loadImages();
                        }
                        ticking = false;
                    });
                    ticking = true;
                }
            };
            const onScroll = handleScroll;
            window.addEventListener('scroll', onScroll, { passive: true });
            this._infiniteScrollCleanup = () => window.removeEventListener('scroll', onScroll, { passive: true });
        }
    }

    // (legacy mobile magnetic snap system removed)

    _evaluateMagnetEnabled() {
        const isMobile = window.matchMedia('(max-width: 600px)').matches;
        const inSettings = this.gallery?.gallery?.classList?.contains('settings-mode');
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


    async renderResetPage() {
        const token = new URLSearchParams(location.search).get('token') || '';
        if (!token) { this.showNotification('Invalid reset link','error'); history.replaceState({}, '', '/'); await this.init(); return; }
        this.gallery.clear();
        // Ensure reset page uses centered settings layout
        if (this.gallery.gallery) {
            this.gallery.gallery.className = 'gallery settings-mode';
        }
        if (this.profileTop) this.profileTop.innerHTML = '';
        const wrap = document.createElement('div'); wrap.className='settings-wrap';
        wrap.innerHTML = `
          <section class="settings-group">
            <div class="settings-label">Reset password</div>
            <input type="password" id="rp-new" class="settings-input" placeholder="New password" autocomplete="new-password"/>
            <div id="pw-strength" aria-live="polite" title="Password strength" style="display:grid;grid-template-columns:repeat(4,1fr);gap:6px">
              <div class="pw-seg"><div class="fill" style="height:6px;width:0%;transition:width .25s"></div></div>
              <div class="pw-seg"><div class="fill" style="height:6px;width:0%;transition:width .25s"></div></div>
              <div class="pw-seg"><div class="fill" style="height:6px;width:0%;transition:width .25s"></div></div>
              <div class="pw-seg"><div class="fill" style="height:6px;width:0%;transition:width .25s"></div></div>
            </div>
            <input type="password" id="rp-confirm" class="settings-input" placeholder="Confirm new password" autocomplete="new-password"/>
            <div class="settings-actions"><button id="rp-save" class="nav-btn">Save</button></div>
          </section>`;
        this.gallery.gallery.appendChild(wrap);
        const pw = document.getElementById('rp-new');
        const pwc = document.getElementById('rp-confirm');
        const meter = wrap.querySelector('#pw-strength');
        // Eye toggles for reveal/hide on both password fields
        const ensureEyeToggle = (input) => {
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
            eye.onclick = () => { const isPass = input.type === 'password'; input.type = isPass ? 'text' : 'password'; };
            wrap.appendChild(eye);
        };
        ensureEyeToggle(pw);
        ensureEyeToggle(pwc);
        const scorePassword = (pwd) => {
            if (!pwd) return 0;
            let categories = 0;
            if (/[a-z]/.test(pwd)) categories++;
            if (/[A-Z]/.test(pwd)) categories++;
            if (/[0-9]/.test(pwd)) categories++;
            if (/[^A-Za-z0-9]/.test(pwd)) categories++;
            const long = pwd.length >= 8;
            if (!long) return Math.min(categories, 1);
            if (categories <= 1) return 1;
            if (categories === 2) return 2;
            if (categories === 3) return 3;
            return 4;
        };
        const renderStrength = (pwd) => {
            const score = scorePassword(pwd);
            if (meter) {
                const segs = meter.querySelectorAll('.pw-seg .fill');
                segs.forEach((seg, i) => { const active = score >= (i + 1); seg.style.width = active ? '100%' : '0%'; seg.classList.toggle('shimmer', score === 4 && i === 3); });
            }
            return score;
        };
        pw.addEventListener('input', (e)=>renderStrength(e.target.value));
        document.getElementById('rp-save').onclick = async () => {
            const a = pw.value.trim(); const b = pwc.value.trim();
            if (renderStrength(a) < 2) { this.showNotification('Password too weak','error'); return; }
            if (a !== b) { this.showNotification('Passwords do not match','error'); return; }
            const r = await fetchWithCSRF('/api/reset-password', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ token, new_password:a }) });
            const d = await r.json().catch(()=>({}));
            if (r.ok && d && d.token && d.user) {
                localStorage.setItem('token', d.token);
                localStorage.setItem('user', JSON.stringify(d.user));
                this.currentUser = d.user;
                this.showNotification('Password updated. You are now signed in.', 'success');
                const username = d.user?.username || '';
                history.pushState({}, '', username ? `/@${encodeURIComponent(username)}` : '/');
                this.init();
            } else {
                const e = d || {}; this.showNotification(e.error||'Reset failed','error');
            }
        };
    }

    async renderVerifyPage() {
        const token = new URLSearchParams(location.search).get('token') || '';
        try { const r = await fetchWithCSRF('/api/verify-email', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ token }) }); if (r.status===204) this.showNotification('Email verified'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Verification failed','error'); } } catch {}
        history.replaceState({}, '', '/'); this.init();
    }


    async renderImagePage(id) {
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.clear();
        this.gallery.gallery.classList.add('settings-mode');
        // Ensure my collected set is hydrated for button state
        await this.seedMyCollectedSet();
        let data = null;
        try {
            const r = await fetchWithCSRF(`/api/images/${encodeURIComponent(id)}`);
            if (!r.ok) throw new Error('not found');
            data = await r.json();
        } catch {
            const wrap = document.createElement('section');
            wrap.className = 'mono-col';
            wrap.style.cssText = 'margin:0 auto 16px;max-width:720px;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--surface-elevated);color:var(--text-primary)';
            wrap.innerHTML = `<div style="font-weight:800;letter-spacing:-0.02em;margin-bottom:6px">Image not found</div><div style="color:var(--text-secondary);font-family:var(--font-mono)">The image may have been removed.</div>`;
            this.gallery.gallery.appendChild(wrap);
            return;
        }
        const wrap = document.createElement('section');
        wrap.className = 'mono-col';
        wrap.style.cssText = 'margin:0 auto 16px;max-width:980px;padding:16px;color:var(--text-primary)';
        const title = ((data.title && String(data.title).trim()) || data.original_name || 'Untitled');
        const username = data.username || 'unknown';
        const asciiFallback = '~ artificial reverie ~';
        const captionText = (data.caption && String(data.caption).trim()) || '';
        const description = (username && captionText)
            ? `by @${username} — ${captionText}`
            : (username && !captionText)
                ? `by @${username} — ${asciiFallback}`
                : (!username && captionText)
                    ? captionText
                    : asciiFallback;
        const captionHtml = data.caption ? `<div class="image-caption" id="single-caption" style="margin-top:8px;color:var(--text-secondary);position:relative">${sanitizeAndRenderMarkdown(String(data.caption))}</div>` : '';
        wrap.innerHTML = `
          <div style="display:grid;gap:12px">
            <div class="single-header">
              <h1 class="single-title" title="${escapeHTML(String(title))}">${escapeHTML(String(title))}</h1>
              <div style="display:flex; align-items:center; gap:8px;">
                <a href="/@${encodeURIComponent(username)}" class="single-username link-btn" style="text-decoration:none">@${escapeHTML(String(username))}</a>
                <button id="single-collect" class="like-btn collect-btn" title="Collect">✧</button>
              </div>
            </div>
            <div style="position:relative;display:flex;justify-content:center">
              <img src="${getImageURL(data.filename)}" alt="${title}" style="max-width:100%;max-height:76vh;border-radius:10px;"/>
            </div>
            ${captionHtml}
          </div>`;
        this.gallery.gallery.appendChild(wrap);

        // Update document title and meta description on client-side navigation
        try {
            const siteTitle = document.querySelector('.logo')?.getAttribute('data-text') || 'TROUGH';
            document.title = `${String(title)} - ${siteTitle}`;
            const ensureMeta = (name) => {
                let m = document.querySelector(`meta[name="${name}"]`);
                if (!m) {
                    m = document.createElement('meta');
                    m.setAttribute('name', name);
                    document.head.appendChild(m);
                }
                return m;
            };
            ensureMeta('description').setAttribute('content', description);

            // Also update OpenGraph and Twitter tags for SPA navigations
            const ensureProp = (prop) => {
                let m = document.querySelector(`meta[property="${prop}"]`);
                if (!m) { m = document.createElement('meta'); m.setAttribute('property', prop); document.head.appendChild(m); }
                return m;
            };
            const ensureName = (n) => ensureMeta(n);

            const imgURL = getImageURL(data.filename);
            const imgAbs = imgURL && imgURL.startsWith('/') ? (location.origin + imgURL) : imgURL;
            const ogType = 'article';
            ensureProp('og:site_name').setAttribute('content', siteTitle);
            ensureProp('og:title').setAttribute('content', String(title));
            ensureProp('og:description').setAttribute('content', description);
            ensureProp('og:type').setAttribute('content', ogType);
            ensureProp('og:url').setAttribute('content', location.href);
            if (imgAbs) {
                ensureProp('og:image').setAttribute('content', imgAbs);
                ensureProp('og:image:alt').setAttribute('content', String(title));
            }

            const twCard = imgAbs ? 'summary_large_image' : 'summary';
            ensureName('twitter:card').setAttribute('content', twCard);
            ensureName('twitter:title').setAttribute('content', String(title));
            ensureName('twitter:description').setAttribute('content', description);
            if (imgAbs) {
                ensureName('twitter:image').setAttribute('content', imgAbs);
                ensureName('twitter:image:alt').setAttribute('content', String(title));
            }
        } catch {}

        // Allow expanding long titles on click (toggle multi-line clamp)
        const titleEl = wrap.querySelector('.single-title');
        if (titleEl) {
            titleEl.setAttribute('role', 'button');
            titleEl.setAttribute('tabindex', '0');
            titleEl.setAttribute('aria-expanded', 'false');
            const toggle = () => {
                const expanded = titleEl.classList.toggle('expanded');
                titleEl.setAttribute('aria-expanded', expanded ? 'true' : 'false');
            };
            titleEl.addEventListener('click', toggle);
            titleEl.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); }
            });
        }

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
        // Wire collect on single image page (disallow owner)
        const collectBtn = document.getElementById('single-collect');
        if (collectBtn) {
            if (this.currentUser && this.currentUser.username === username) {
                collectBtn.style.display = 'none';
            } else {
                // Reflect cache if present
                if (this._myCollectedSet && this._myCollectedSet.has(String(data.id))) { collectBtn.classList.add('collected'); collectBtn.textContent = '✦'; }
                else { collectBtn.classList.remove('collected'); collectBtn.textContent = '✧'; }
                collectBtn.onclick = async () => {
                    await this.toggleCollect(data.id, collectBtn);
                    if (!this._myCollectedSet) this._myCollectedSet = new Set();
                    if (collectBtn.classList.contains('collected')) this._myCollectedSet.add(String(data.id)); else this._myCollectedSet.delete(String(data.id));
                };
            }
        }
    }

    // Render a CMS page by slug; returns true if handled
    async renderCMSPage(slug) {
        try {
            const r = await fetchWithCSRF(`/api/pages/${encodeURIComponent(slug)}`);
            if (!r.ok) return false;
            const d = await r.json().catch(()=>null);
            if (!d) return false;
            if (d.redirect_url) { window.location.href = d.redirect_url; return true; }
            if (this.profileTop) this.profileTop.innerHTML = '';
            this.gallery.clear();
            this.gallery.gallery.classList.add('settings-mode');
            const wrap = document.createElement('section');
            wrap.className = 'mono-col';
            wrap.style.cssText = 'margin:0 auto 16px;max-width:980px;padding:16px;color:var(--text-primary)';
            const isAdmin = !!this.currentUser?.is_admin;
            const editBtn = isAdmin ? '<button id="page-edit" class="link-btn" style="justify-self:end">Edit</button>' : '';
            wrap.innerHTML = `
              <div class="page-wrap" style="display:grid;gap:12px">
                <div class="page-toolbar" style="display:flex;justify-content:flex-end">${editBtn}</div>
                <article class="page-content"></article>
              </div>`;
            this.gallery.gallery.appendChild(wrap);
            const art = wrap.querySelector('.page-content');
            if (art) {
                let html = String(d.html||'');
                if (!html && d.markdown) {
                    const raw = String(d.markdown||'');
                    try {
                        if (window.markdownit) {
                            const slugify = (s) => String(s).toLowerCase().trim().replace(/[^a-z0-9\s-]/g,'').replace(/\s+/g,'-');
                            const md = window.markdownit({ html: true, linkify: true, breaks: true });
                            if (window.markdownitFootnote) md.use(window.markdownitFootnote);
                            if (window.markdownitContainer) {
                                ['note','info','tip','warning','danger','success','quote'].forEach(name => {
                                    md.use(window.markdownitContainer, name, {
                                        render: (tokens, idx) => tokens[idx].nesting === 1 ? `<div class=\"admon admon-${name}\">` : `</div>`
                                    });
                                });
                                md.use(window.markdownitContainer, 'details', {
                                    render: (tokens, idx) => {
                                        if (tokens[idx].nesting === 1) {
                                            const info = tokens[idx].info.trim().slice('details'.length).trim();
                                            const title = info || 'Details';
                                            return `<details class=\"md-details\"><summary>${title}</summary>`;
                                        } else { return `</details>`; }
                                    }
                                });
                            }
                            if (window.markdownitAnchor) md.use(window.markdownitAnchor, { slugify });
                            let src = raw;
                            if (src.includes('[[TOC]]')) {
                                try {
                                    const tmp = window.markdownit({ html:false });
                                    const headings = [];
                                    tmp.core.ruler.push('collect_headings', state => {
                                        state.tokens.forEach((t,i) => {
                                            if (t.type === 'heading_open') {
                                                const level = Number(t.tag.slice(1));
                                                const inline = state.tokens[i+1];
                                                const text = (inline && inline.type==='inline') ? inline.content : '';
                                                const slug = slugify(text||'');
                                                headings.push({ level, text, slug });
                                            }
                                        });
                                    });
                                    tmp.render(src);
                                    if (headings.length) {
                                        const toc = '<nav class="page-toc"><ul>' + headings.map(h=>`<li class="lv${h.level}"><a href="#${h.slug}">${escapeHTML(String(h.text||''))}</a></li>`).join('') + '</ul></nav>';
                                        src = src.replace('[[TOC]]', toc);
                                    }
                                } catch {}
                            }
                            html = md.render(src);
                        } else if (window.marked) {
                            if (window.marked.setOptions) { window.marked.setOptions({ gfm: true, breaks: true }); }
                            html = window.marked.parse(raw);
                        } else {
                            html = sanitizeAndRenderMarkdown(raw);
                        }
                    } catch { html = sanitizeAndRenderMarkdown(raw); }
                }
                try { if (window.DOMPurify) { html = window.DOMPurify.sanitize(html, { ADD_ATTR: ['id','class','name'] }); } } catch {}
                art.innerHTML = html;
                // External links only: open in new tab
                Array.from(art.querySelectorAll('a[href]')).forEach(a => {
                    const href = a.getAttribute('href') || '';
                    if (href.startsWith('#') || href.startsWith('/')) return;
                    try {
                        const u = new URL(href, location.href);
                        if (u.origin !== location.origin) { a.setAttribute('target','_blank'); a.setAttribute('rel','noopener nofollow'); }
                    } catch {}
                });
                // In-page anchor scroll (footnotes, TOC) with nav offset
                art.addEventListener('click', (e) => {
                    const a = e.target && e.target.closest ? e.target.closest('a[href^="#"]') : null;
                    if (!a) return;
                    e.preventDefault();
                    const id = decodeURIComponent((a.getAttribute('href')||'').slice(1));
                    if (!id) return;
                    const target = art.querySelector(`[id="${id}"]`);
                    if (!target) { location.hash = `#${id}`; return; }
                    const nav = document.getElementById('nav');
                    const offset = (nav && nav.getBoundingClientRect().height) ? nav.getBoundingClientRect().height + 8 : 0;
                    const y = target.getBoundingClientRect().top + window.scrollY - offset;
                    try { window.scrollTo({ top: Math.max(0, y), behavior: 'smooth' }); } catch { window.scrollTo(0, Math.max(0, y)); }
                    // Update hash without jumping
                    try { history.pushState({}, '', `#${id}`); } catch {}
                }, { once: true });
            }
            // Update title for SPA nav; inherit index SEO for everything else
            try {
                const siteTitle = document.querySelector('.logo')?.getAttribute('data-text') || 'TROUGH';
                const newTitle = (d.meta_title && String(d.meta_title).trim()) || `${String(d.title||'Page')} - ${siteTitle}`;
                document.title = newTitle;
                // Re-apply site default OG/Twitter meta but with the page title and URL
                this.applySiteDefaultMeta({ overrideTitle: newTitle, overrideUrl: location.href });
            } catch {}
            // Inline page editor for admins
            const edit = document.getElementById('page-edit');
            if (edit) {
                edit.onclick = async () => {
                    // Find page id by slug via admin list
                    let pid = null; let pageRow = null;
                    try {
                        const rr = await fetchWithCSRF('/api/admin/pages?limit=200', { credentials: 'include' });
                        if (rr.ok) {
                            const dd = await rr.json().catch(()=>({pages:[]}));
                            (dd.pages||[]).forEach(p => { if (String(p.slug||'') === String(slug)) { pid = p.id; pageRow = p; } });
                        }
                    } catch {}
                    if (!pid) { this.showNotification('Unable to load page for editing','error'); return; }
                    const overlay = document.createElement('div');
                    overlay.style.cssText='position:fixed;inset:0;z-index:3000;background:rgba(0,0,0,0.6);backdrop-filter:blur(6px);display:flex;align-items:center;justify-content:center;padding:24px;';
                    const panel = document.createElement('div');
                    panel.style.cssText='max-width:900px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
                    panel.innerHTML = `
                      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
                        <div class="settings-label">Edit Page</div>
                        <button id="pgx-close" class="link-btn">Close</button>
                      </div>
                      <div style="display:grid;gap:8px">
                        <div style="display:grid;gap:6px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
                          <div style="display:grid;gap:6px"><label class="settings-label">Slug</label><input id="pgx-slug" class="settings-input" disabled value="${escapeHTML(String(slug))}"/></div>
                          <div style="display:grid;gap:6px"><label class="settings-label">Title</label><input id="pgx-title" class="settings-input" value="${escapeHTML(String(pageRow?.title||''))}"/></div>
                          <div style="display:grid;gap:6px"><label class="settings-label">Redirect URL</label><input id="pgx-redirect" class="settings-input" value="${escapeHTML(String(pageRow?.redirect_url||''))}" placeholder="https://..."/></div>
                        </div>
                        <div style="display:grid;gap:6px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
                          <div style="display:grid;gap:6px"><label class="settings-label">Meta title</label><input id="pgx-meta-title" class="settings-input" value="${escapeHTML(String(d.meta_title||''))}"/></div>
                          <div style="display:grid;gap:6px"><label class="settings-label">Meta description</label><input id="pgx-meta-desc" class="settings-input" value="${escapeHTML(String(d.meta_description||''))}"/></div>
                        </div>
                        <label style="display:flex;gap:8px;align-items:center"><input id="pgx-pub" type="checkbox" ${pageRow?.is_published? 'checked':''}/> Published</label>
                        <div style="display:grid;gap:6px"><label class="settings-label">Content (Markdown)</label><textarea id="pgx-md" class="settings-input" style="min-height:320px">${escapeHTML(String(d.markdown||''))}</textarea></div>
                        <div class="settings-actions" style="gap:8px;align-items:center;justify-content:flex-end"><button id="pgx-save" class="nav-btn">Save</button></div>
                      </div>`;
                    overlay.appendChild(panel); document.body.appendChild(overlay);
                    const close = () => overlay.remove();
                    panel.querySelector('#pgx-close').onclick = close;
                    panel.querySelector('#pgx-save').onclick = async () => {
                        const body = {
                            slug: slug,
                            title: panel.querySelector('#pgx-title').value,
                            markdown: panel.querySelector('#pgx-md').value.replace(/\r\n/g,'\n'),
                            is_published: panel.querySelector('#pgx-pub').checked,
                            redirect_url: (panel.querySelector('#pgx-redirect').value||'').trim() || null,
                            meta_title: panel.querySelector('#pgx-meta-title').value,
                            meta_description: panel.querySelector('#pgx-meta-desc').value,
                        };
                        try {
                            const rr = await fetchWithCSRF(`/api/admin/pages/${pid}`, { method:'PUT', headers:{ 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify(body) });
                            if (!rr.ok) { const e = await rr.json().catch(()=>({})); this.showNotification(e.error||'Save failed','error'); return; }
                            this.showNotification('Saved'); close();
                            // Re-render page with latest
                            await this.renderCMSPage(slug);
                        } catch { this.showNotification('Save failed','error'); }
                    };
                };
            }
            return true;
        } catch { return false; }
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

            const response = await fetchWithCSRF('/api/admin/site/export-uploads', {
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


document.addEventListener('DOMContentLoaded', () => { window.app = new TroughApp(); });