// PREMIUM GALLERY APPLICATION
class TroughApp {
    constructor() {
        this.images = [];
        this.page = 1;
        this.loading = false;
        this.hasMore = true;
        this.currentUser = null;
        // Image lazy-loading state
        this.imageObserver = null;
        this.lazyQueue = [];
        this.currentImageLoads = 0;
        this.maxConcurrentImageLoads = 4; // cap concurrent downloads to reduce server stress
        // Managed masonry layout state
        this.masonry = {
            enabled: false,
            columnCount: 0,
            columns: [],
            nextColumnIndex: 0,
            appendedCount: 0,
            estimatedHeights: [],
            columnWidth: 0,
            rowHeightHint: 0,
        };
        // (legacy magnetic scroll state removed)
        this._lastScrollSaveTs = 0;
        this.isRestoring = false;
        // Rendering control to avoid stale inserts across route changes
        this.renderEpoch = 0;
        this.pendingTimers = new Set();
        this.routeMode = 'home';
        // Queue of items not yet rendered as cards (for chunked reveal)
        this.unrendered = [];
        // Interaction flag for conservative prefill before any user action
        this._userInteracted = false;
        // Optional override to force which masonry column the next card should use
        this._forcedColumnIndex = null;
		// Track my collected image ids for UI state
		this._myCollectedSet = new Set();
        
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

        // Initialize drift-based MagneticScroll once for the whole app lifecycle
        this.magneticScroll = new MagneticScroll({
            minCardIndex: 0,
            attractionStrength: 0.012,
            damping: 0.94,
            maxDriftSpeed: 0.6,
            settleDelay: 100,
            effectiveRange: 250,
        });

        const cachedUser = localStorage.getItem('user');
        if (cachedUser) {
            try { this.currentUser = JSON.parse(cachedUser); } catch {}
            if (this.currentUser?.username) {
                this.authBtn.textContent = `@${this.currentUser.username}`;
                this.authBtn.title = `@${this.currentUser.username}`;
                this.authBtn.style.fontFamily = 'var(--font-mono)';
            }
        }
        
        this.init();
    }

    async seedMyCollectedSet() {
        try {
            // Only for logged-in users
            if (!this.currentUser || !this.currentUser.username) { this._myCollectedSet = new Set(); return; }
            // Fetch first page of my collections to initialize state
            const resp = await fetch(`/api/users/${encodeURIComponent(this.currentUser.username)}/collections?page=1`, { credentials: 'include' });
            if (!resp.ok) { this._myCollectedSet = this._myCollectedSet || new Set(); return; }
            const data = await resp.json();
            this._myCollectedSet = new Set((data.images || []).map(img => String(img.id)));
        } catch {
            this._myCollectedSet = this._myCollectedSet || new Set();
        }
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
        await this.checkAuth();
		// Seed my collection state early for correct UI on first paint
		await this.seedMyCollectedSet();
        await this.applyPublicSiteSettings();
        this.setupHistoryHandler();
        this.setupEventListeners();
        this.setupImageLazyLoader();

        if (location.pathname === '/reset') { await this.renderResetPage(); return; }
        if (location.pathname === '/verify') { await this.renderVerifyPage(); return; }
        if (location.pathname.startsWith('/@')) {
            const username = decodeURIComponent(location.pathname.slice(2));
            this.beginRender('profile');
            await this.renderProfilePage(username);
            return;
        }
		// CMS pages (single-segment slugs)
		if (/^\/[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$/.test(location.pathname)) {
			const slug = location.pathname.slice(1);
			const ok = await this.renderCMSPage(slug);
			if (ok) return;
		}
        if (location.pathname === '/settings') {
            this.beginRender('settings');
            await this.renderSettingsPage();
            return;
        }
        if (location.pathname === '/admin') {
            this.beginRender('admin');
            await this.renderAdminPage();
            return;
        }
        if (location.pathname === '/register') {
            // Open auth modal directly on register tab and capture invite
            const url = new URL(location.href);
            const invite = url.searchParams.get('invite');
            this.showAuthModal();
            const tabs = document.querySelectorAll('.auth-tab');
            const registerTab = Array.from(tabs).find(t => t.dataset.tab === 'register');
            const loginTab = Array.from(tabs).find(t => t.dataset.tab === 'login');
            const proceed = async () => {
                const allowRegister = window.__PUBLIC_REG_ENABLED__ !== false;
                if (allowRegister && registerTab) {
                    tabs.forEach(t => t.classList.remove('active')); registerTab.classList.add('active');
                    const loginForm = document.getElementById('login-form'); const registerForm = document.getElementById('register-form'); const submitBtn = document.getElementById('auth-submit');
                    if (loginForm && registerForm && submitBtn) { loginForm.style.display='none'; registerForm.style.display='block'; submitBtn.textContent='Create Account'; }
                } else if (loginTab) {
                    tabs.forEach(t => t.classList.remove('active')); loginTab.classList.add('active');
                }
            };
            if (invite) {
                try {
                    const r = await fetch(`/api/validate-invite?code=${encodeURIComponent(invite)}`);
                    if (r.status === 204) {
                        this._pendingInvite = invite;
                        if (registerTab) {
                            tabs.forEach(t => t.classList.remove('active')); registerTab.classList.add('active');
                            const loginForm = document.getElementById('login-form'); const registerForm = document.getElementById('register-form'); const submitBtn = document.getElementById('auth-submit');
                            if (loginForm && registerForm && submitBtn) { loginForm.style.display='none'; registerForm.style.display='block'; submitBtn.textContent='Create Account'; }
                        }
                    } else {
                        this.showNotification('Invalid invitation link', 'error');
                        await proceed();
                    }
                } catch {
                    this.showNotification('Unable to validate invite', 'error');
                    await proceed();
                }
            } else {
                await proceed();
            }
            return;
        }
        if (location.pathname.startsWith('/i/')) {
            this.beginRender('image');
            const id = location.pathname.split('/')[2];
            await this.renderImagePage(id);
            return;
        }
        // Not a profile/settings page, clear profileTop
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.beginRender('home');
        this.enableManagedMasonry();
        await this.loadImages();
        this.setupInfiniteScroll();
        // Ensure logo data-text mirrors current text for blend-mode rendering
        const logo = document.querySelector('.logo');
        if (logo && !logo.getAttribute('data-text')) {
            logo.setAttribute('data-text', logo.textContent || '');
        }
        // Ensure MagneticScroll is enabled for the home feed
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) {
            this.magneticScroll.updateEnabledState();
        }
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
                this.gallery.classList.remove('settings-mode');
                this.gallery.innerHTML = '';
                if (this.profileTop) this.profileTop.innerHTML = '';
                this.page = 1;
                this.hasMore = true;
                await this.loadImages();
            } else if (path.startsWith('/@')) {
                // Profile pages fetch page 1 only; rendering is handled by caller
                try { if (typeof state?.scrollY === 'number') window.scrollTo(0, state.scrollY); } catch {}
            }
            return;
        }

        if (path === '/') {
            // Rebuild the home feed up to the saved page
            this.gallery.classList.remove('settings-mode');
            this.gallery.innerHTML = '';
            if (this.profileTop) this.profileTop.innerHTML = '';
            this.page = 1;
            this.hasMore = true;
            this.isRestoring = true;
            const targetPage = Math.max(1, state.page);
            while (this.page <= targetPage && this.hasMore) {
                await this.loadImages();
                // Immediately reveal a small batch so the anchor card exists in DOM
                this.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
            }
            this.isRestoring = false;
            // Prefer anchoring by id, then index; fallback to scrollY
            let targetCard = null;
            if (state.firstVisibleId) {
                targetCard = document.querySelector(`.image-card[data-image-id="${CSS.escape(String(state.firstVisibleId))}"]`);
            }
            if (!targetCard && Number.isFinite(state.firstVisibleIndex) && state.firstVisibleIndex > 0) {
                const cards = Array.from(document.querySelectorAll('.image-card'));
                // If the indexed card doesn't exist yet, try revealing until it does or we run out
                while (!cards[state.firstVisibleIndex] && (this.unrendered && this.unrendered.length)) {
                    const revealed = this.maybeRevealCards((window.innerWidth <= 600) ? 6 : 10);
                    if (!revealed) break;
                }
                const cards2 = Array.from(document.querySelectorAll('.image-card'));
                targetCard = cards2[state.firstVisibleIndex] || null;
            }
            if (targetCard) {
                const rect = targetCard.getBoundingClientRect();
                const y = rect.top + window.scrollY - (document.getElementById('nav')?.offsetHeight || 0) - 8;
                try { window.scrollTo(0, Math.max(0, y)); } catch {}
                // After anchoring, ensure there is sufficient content below to allow further scrolling
                await this.topUpBelowViewport(4);
            } else if (typeof state.scrollY === 'number') {
                try { window.scrollTo(0, state.scrollY); } catch {}
                await this.topUpBelowViewport(4);
            }
        } else if (path.startsWith('/@')) {
            // For profiles, fetch the page 1 images, then scroll to saved position
            this.isRestoring = true;
            try {
                // Re-render profile (page 1 already fetched by renderProfilePage when navigated via back)
                // If we are on a fresh load and calling restore explicitly, ensure DOM exists
                const username = decodeURIComponent(path.slice(2));
                if (!document.querySelector('.image-card')) {
                    await this.renderProfilePage(username);
                }
            } catch {}
            this.isRestoring = false;
            if (typeof state.scrollY === 'number') { try { window.scrollTo(0, state.scrollY); } catch {} }
        }
    }

    setupHistoryHandler() {
        window.onpopstate = async () => {
            // Bump epoch at the start of any history-driven navigation
            this.beginRender(
                location.pathname === '/' ? 'home' :
                location.pathname.startsWith('/@') ? 'profile' :
                (location.pathname === '/settings' ? 'settings' : (location.pathname === '/admin' ? 'admin' : 'image'))
            );
            if (location.pathname.startsWith('/i/')) {
                const id2 = location.pathname.split('/')[2];
                await this.renderImagePage(id2);
            } else if (location.pathname.startsWith('/@')) {
                const u = decodeURIComponent(location.pathname.slice(2));
                // Render synchronously for restoration to avoid duplicate/staggered inserts
                this.isRestoring = true;
                // Ensure multi-column gallery mode for profiles
                this.gallery.classList.remove('settings-mode');
                await this.renderProfilePage(u);
                await this.restoreListState(location.pathname);
                this.isRestoring = false;
            } else if (location.pathname === '/settings') {
                await this.renderSettingsPage();
            } else if (location.pathname === '/admin') {
                await this.renderAdminPage();
            } else {
                // Home feed: restore saved state if present
                await this.restoreListState('/');
                // Ensure home SEO sticks after SPA/back nav
                try {
                    const cached = localStorage.getItem('site_settings');
                    let s = null; try { s = JSON.parse(cached||'null'); } catch { s = null; }
                    const siteTitle = document.querySelector('.logo')?.getAttribute('data-text') || s?.site_name || 'TROUGH';
                    const title = s?.seo_title || `${siteTitle} · AI IMAGERY`;
                    document.title = title;
                    this.applySiteDefaultMeta({ overrideTitle: title, overrideUrl: location.href });
                } catch {}
            }
            if (this.magneticScroll && this.magneticScroll.updateEnabledState) {
                this.magneticScroll.updateEnabledState();
            }

            // After any route change, normalize gallery mode classes:
            if (location.pathname.startsWith('/i/')) {
                this.gallery.classList.add('settings-mode');
            } else {
                this.gallery.classList.remove('settings-mode');
            // Ensure managed masonry on list-like pages
            if (this.routeMode === 'home' || this.routeMode === 'profile') {
                this.enableManagedMasonry();
            } else {
                this.disableManagedMasonry();
            }
            }
        };
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
            // Reset lazy loading pipeline
            try { if (this.imageObserver) this.imageObserver.disconnect(); } catch {}
            this.lazyQueue = [];
            this.currentImageLoads = 0;
            this.unrendered = [];
            // Reset managed masonry layout
            this.disableManagedMasonry();
            // Stop any in-flight scroll animations
            if (this._activeScrollAnim) { this._activeScrollAnim.cancelled = true; this._activeScrollAnim = null; }
        } catch {}
    }

    trackTimeout(id) { try { if (id) this.pendingTimers.add(id); } catch {} return id; }
    untrackTimeout(id) { try { if (id) this.pendingTimers.delete(id); } catch {} }

    // IntersectionObserver + small queue for seamless lazy loading
    setupImageLazyLoader() {
        // Fallback: if no IO, we'll just let native lazy do the work
        if (!('IntersectionObserver' in window)) return;
        try {
			// Preload a comfortable buffer below the viewport so scrolling feels instant
        const isMobile = window.innerWidth <= 600;
        const rootMargin = (isMobile ? '200px 0px 200px 0px' : '700px 0px 700px 0px');
            this.imageObserver = new IntersectionObserver((entries) => {
                for (const entry of entries) {
                    // We observe the card host to avoid zero-area issues; map back to the image
                    const target = entry.target;
                    const img = target._lazyImg || target;
                    if (entry.isIntersecting) {
                        this.imageObserver.unobserve(target);
                        if (target._lazyImg) delete target._lazyImg;
                        this.enqueueImageLoad(img);
                    }
                }
            }, { root: null, threshold: 0.01, rootMargin });
        } catch {}
    }

    registerImageForLazyLoad(img) {
        // If no observer, set src immediately as a graceful fallback
        if (!this.imageObserver) {
            this.loadImageNow(img, { eager: true, highPriority: true });
            return;
        }

        // Measure using the card container to avoid zero-height issues before image paints
        const host = img.closest('.image-card') || img;
        const rect = host.getBoundingClientRect();
        const viewportH = window.innerHeight || document.documentElement.clientHeight;
		const isMobile = window.innerWidth <= 600;
        
        // Load images that are above the fold immediately with high priority
        // Be generous with "above the fold" to ensure instant visibility
        const aboveFoldPad = isMobile ? 50 : 100;
        if (rect.top < viewportH + aboveFoldPad) {
            this.loadImageNow(img, { eager: true, highPriority: true });
            return;
        }
        
        // Also prioritize the first row regardless of position (for fast initial paint)
        // Use seqIndex when available; fallback to DOM index otherwise
        let visualIndex = Number.isFinite(Number(host?.dataset?.seqIndex)) ? Number(host.dataset.seqIndex) : -1;
        if (visualIndex < 0) {
            if (this.masonry && this.masonry.enabled && this.masonry.columns?.length) {
                const all = [];
                for (const col of this.masonry.columns) all.push(...Array.from(col.children));
                visualIndex = all.indexOf(host);
            } else {
                visualIndex = Array.from(this.gallery.children).indexOf(host);
            }
        }
		const eagerThresholdBase = (this.masonry && this.masonry.enabled && this.masonry.columnCount) ? this.masonry.columnCount : 4;
		const eagerThreshold = isMobile ? Math.min(2, eagerThresholdBase) : eagerThresholdBase;
        if (visualIndex > -1 && visualIndex < eagerThreshold) {
            this.loadImageNow(img, { eager: true, highPriority: true });
            return;
        }

        // Load images just below the fold (buffer zone) with normal priority
        const buffer = this.computeLazyBuffer();
        if (rect.top < viewportH + buffer) {
            this.enqueueImageLoad(img, /*preferFront*/ true);
            return;
        }

        // For images further down, observe them and load when they approach
        host._lazyImg = img;
        this.imageObserver.observe(host);

		// Safety: re-check after layout settles in case positions changed
        requestAnimationFrame(() => {
            if (img.dataset.loaded === '1' || img.dataset.loading === '1' || img.dataset.queued === '1') return;
            const h = img.closest('.image-card') || img;
            const r = h.getBoundingClientRect();
            const vh = window.innerHeight || document.documentElement.clientHeight;
            // If now in buffer zone, add to queue
            const buf = this.computeLazyBuffer();
            if (r.top < vh + buf) {
                this.enqueueImageLoad(img, true);
            }
        });
    }

    enqueueImageLoad(img, preferFront = false) {
        if (img.dataset.loading === '1' || img.dataset.loaded === '1' || img.dataset.queued === '1') return;
        // Keep queue ordered by distance to viewport so closer images are loaded first
        img.dataset.queuedAt = String(performance.now());
        img.dataset.queued = '1';
        if (preferFront) {
            this.lazyQueue.unshift(img);
        } else {
            this.lazyQueue.push(img);
        }
        this.processLazyQueue();
    }

    processLazyQueue() {
        if (this.currentImageLoads >= this.maxConcurrentImageLoads) return;
        if (this.lazyQueue.length === 0) return;

        // Sort by absolute distance to viewport top for better perceived performance
        const viewportTop = 0;
        const viewportBottom = (window.innerHeight || document.documentElement.clientHeight);
        const viewportCenter = (viewportTop + viewportBottom) / 2;
        this.lazyQueue.sort((a, b) => {
            const da = Math.abs((a.getBoundingClientRect().top || 0) - viewportCenter);
            const db = Math.abs((b.getBoundingClientRect().top || 0) - viewportCenter);
            return da - db;
        });

        while (this.currentImageLoads < this.maxConcurrentImageLoads && this.lazyQueue.length > 0) {
            const nextImg = this.lazyQueue.shift();
            if (!nextImg) break;
            if (nextImg.dataset.loading === '1' || nextImg.dataset.loaded === '1') continue;
            nextImg.dataset.queued = '0';
            this.loadImageNow(nextImg);
        }
    }

    loadImageNow(img, options = {}) {
        const { eager = false, highPriority = false } = options;
        if (img.dataset.loading === '1' || img.dataset.loaded === '1') return;
        const src = img.dataset.src;
        if (!src) return;

        img.dataset.loading = '1';
        this.currentImageLoads++;

        // For high priority images, bypass the queue system entirely
        if (highPriority) {
            if (eager) img.loading = 'eager';
            img.setAttribute('fetchpriority', 'high');
        } else {
            img.loading = 'lazy';
        }
        img.decoding = 'async';

        const finalize = () => {
            img.dataset.loaded = '1';
            img.dataset.loading = '0';
            // Smooth reveal
            if (!img.style.transition) {
                img.style.transition = 'opacity 180ms var(--ease-out, ease-out)';
            }
            img.style.opacity = '1';
            this.currentImageLoads = Math.max(0, this.currentImageLoads - 1);
            // Kick the queue in case more slots free up
            this.processLazyQueue();
        };

        const onError = () => {
            img.dataset.loading = '0';
            img.style.opacity = '1'; // Show placeholder even on error
            this.currentImageLoads = Math.max(0, this.currentImageLoads - 1);
            this.processLazyQueue();
        };

        // Start the request
        img.src = src;

        // Prefer decode() for a clean paint if available
        if (typeof img.decode === 'function') {
            img.decode().then(finalize).catch(finalize);
        } else {
            img.onload = finalize;
            img.onerror = onError;
        }
    }

    async applyPublicSiteSettings() {
        try {
            const r = await fetch('/api/site');
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

    async checkAuth() {
        const token = localStorage.getItem('token');
        // First try cookie-based session
        try {
            const resp = await fetch('/api/me', { credentials: 'include' });
            if (resp.ok) {
                const data = await resp.json();
                this.currentUser = data.user;
                localStorage.setItem('user', JSON.stringify(data.user));
                this.updateAuthButton();
                return;
            }
        } catch {}
        // Fallback: try bearer token (useful on mobile HTTP where cookies may be blocked)
        if (token) {
            try {
                const resp2 = await fetch('/api/me', { credentials: 'include', headers: { 'Authorization': `Bearer ${token}` } });
                if (resp2.ok) {
                    const data2 = await resp2.json();
                    this.currentUser = data2.user;
                    localStorage.setItem('user', JSON.stringify(data2.user));
                    this.updateAuthButton();
                    return;
                }
            } catch {}
        }
        // If both fail, ensure local logged-out state without pinging server logout
        try { localStorage.removeItem('user'); } catch {}
        this.currentUser = null;
        this.updateAuthButton();
    }

    updateAuthButton() {
        if (this.currentUser) {
            this.authBtn.textContent = `@${this.currentUser.username}`;
            this.authBtn.title = `@${this.currentUser.username}`;
            this.authBtn.style.fontFamily = 'var(--font-mono)';
            // Refresh my collected set after login
            this.seedMyCollectedSet().catch(()=>{});
        } else {
            this.authBtn.textContent = 'ENTER';
            this.authBtn.style.fontFamily = '';
            // Clear collected cache when logged out
            this._myCollectedSet = new Set();
        }
    }

    setupEventListeners() {
        // Profile button always goes to profile
        this.authBtn.addEventListener('click', async () => {
            if (this.currentUser) {
                try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.persistListState(); } catch {}
                history.pushState({}, '', `/@${encodeURIComponent(this.currentUser.username)}`);
                await this.renderProfilePage(this.currentUser.username);
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
            history.pushState({}, '', href);
            if (href.startsWith('/i/')) {
                const id = href.split('/')[2];
            await this.renderImagePage(id);
            // Ensure single-image is in single-column mode only
            this.gallery.classList.add('settings-mode');
            } else if (href.startsWith('/@')) {
                const u = decodeURIComponent(href.slice(2));
                // Explicit navigation to profile should be fresh (no restore)
                await this.renderProfilePage(u);
                this.enableManagedMasonry();
                window.scrollTo(0, 0);
            } else if (href === '/settings') {
                await this.renderSettingsPage();
            } else if (href === '/admin') {
                await this.renderAdminPage();
            } else if (href === '/') {
                // Explicit navigation to home should be fresh (no restore)
                this.gallery.classList.remove('settings-mode');
                this.gallery.innerHTML = '';
                if (this.profileTop) this.profileTop.innerHTML = '';
                this.page = 1; this.hasMore = true;
                window.scrollTo(0, 0);
                this.beginRender('home');
                this.enableManagedMasonry();
                this.applyHomeSEO();
                await this.seedMyCollectedSet();
                await this.loadImages();
                this.setupInfiniteScroll();
            } else if (/^\/[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$/.test(href)) {
                // Single-segment CMS page
                const slug = href.slice(1);
                const ok = await this.renderCMSPage(slug);
                if (!ok) {
                    // Fallback to hard navigation if render failed
                    location.href = href;
                }
            }
        };
        if (this.gallery) this.gallery.addEventListener('click', handleInternalLink, true);
        if (this.profileTop) this.profileTop.addEventListener('click', handleInternalLink, true);
        const nav = document.getElementById('nav');
        if (nav) nav.addEventListener('click', handleInternalLink, true);

        // Intercept logo click to SPA-navigate home (fresh)
        const logo = document.querySelector('.logo');
        if (logo) {
            logo.addEventListener('click', async (e) => {
                e.preventDefault();
                e.stopPropagation();
                e.stopImmediatePropagation();
                history.pushState({}, '', '/');
                this.gallery.classList.remove('settings-mode');
                this.gallery.innerHTML = '';
                if (this.profileTop) this.profileTop.innerHTML = '';
                this.page = 1; this.hasMore = true;
                // Scroll to top synchronously before loading, to avoid race with magnetic/IO
                try { window.scrollTo(0, 0); } catch {}
                this.beginRender('home');
                this.applyHomeSEO();
                // Ensure my collected set is fresh so feed buttons render correctly
                await this.seedMyCollectedSet();
                await this.loadImages();
                this.setupInfiniteScroll();
            }, true);
        }
    }

    // Sign out clears auth and updates UI
    async signOut() {
        // Clear server-side cookie session; keepalive ensures it completes during navigation
        try { await fetch('/api/logout', { method: 'POST', credentials: 'include', keepalive: true }); } catch {}
        try { localStorage.removeItem('token'); localStorage.removeItem('user'); } catch {}
        this.currentUser = null;
        this.updateAuthButton();
        this._myCollectedSet = new Set();
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
            let categories = 0;
            if (/[a-z]/.test(pwd)) categories++;
            if (/[A-Z]/.test(pwd)) categories++;
            if (/[0-9]/.test(pwd)) categories++;
            if (/[^A-Za-z0-9]/.test(pwd)) categories++;
            const long = pwd.length >= 8;
            // 4 levels mapped to 0-4: 0 empty/very weak, 1 weak, 2 fair, 3 good, 4 strong
            if (!long) return Math.min(categories, 1); // cap to weak if short
            if (categories <= 1) return 1;
            if (categories === 2) return 2;
            if (categories === 3) return 3;
            return 4;
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
        this.showLoader(); this.hideAuthError();
        try {
            const response = await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify({ email, password }) });
            const data = await response.json().catch(() => ({}));
            if (response.ok) {
                // Prefer cookie-based session; still cache user locally for UI
                try {
                    if (data && data.token) localStorage.setItem('token', data.token);
                } catch {}
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
        const invite = this._pendingInvite || new URL(location.href).searchParams.get('invite') || '';

        // Client-side reserved usernames (mirrors server policy) to provide instant feedback
        const RESERVED = new Set([
            'admin','administrator','adminteam','admins','root','system','sysadmin','superadmin','superuser',
            'support','help','helpdesk','moderator','mod','mods','staff','team','security','official',
            'noreply','no-reply','postmaster','abuse','report','reports','owner','undefined','null'
        ]);
        if (RESERVED.has(username.toLowerCase())) {
            this.showAuthError('Username unavailable');
            this.showNotification('Username unavailable', 'error');
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

        this.showLoader();
        this.hideAuthError();

        // Preflight: if username already exists, short-circuit with friendly error
        try {
            const existsResp = await fetch(`/api/users/${encodeURIComponent(username)}`);
            if (existsResp && existsResp.ok) {
                this.hideLoader();
                this.showAuthError('Username unavailable');
                this.showNotification('Username unavailable', 'error');
                return;
            }
        } catch {}

        try {
            const response = await fetch('/api/register' + (invite ? ('?invite=' + encodeURIComponent(invite)) : ''), {
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
                this.closeAuthModal();
                this.updateAuthButton();
                this.showNotification(`Welcome to TROUGH, ${data.user.username}!`, 'success');
                this._pendingInvite = '';
                try {
                    history.pushState({}, '', `/@${encodeURIComponent(data.user.username)}`);
                    await this.renderProfilePage(data.user.username);
                } catch {}
            } else {
                const err = (data && typeof data.error === 'string') ? data.error : '';
                if (response.status === 409) {
                    if (/email/i.test(err)) {
                        this.showAuthError('Email already registered');
                        this.showNotification('Email already registered', 'error');
                    } else {
                        this.showAuthError('Username unavailable');
                        this.showNotification('Username unavailable', 'error');
                    }
                } else if (response.status === 400 && /\busername\b/i.test(err) && /(reserved|taken|unavailable)/i.test(err)) {
                    this.showAuthError('Username unavailable');
                    this.showNotification('Username unavailable', 'error');
                } else {
                    this.showAuthError(err || 'Registration failed');
                }
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
        this.beginRender('profile');
        // Ensure gallery uses multi-column layout (remove single-column mode from image page)
        this.gallery.classList.remove('settings-mode');
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.innerHTML = '';
        // Enable managed masonry after clearing content
        this.enableManagedMasonry();
        // Ensure we start at page 1 for profile images when rendering fresh
        this.page = 1;
        this.hasMore = true;

        // Ensure MagneticScroll is enabled for profile pages
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) {
            this.magneticScroll.updateEnabledState();
        }

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
              <div style="color:var(--text-secondary);font-family:var(--font-mono);line-height:1.6">The profile <strong>@${this.escapeHTML(String(username))}</strong> does not exist.</div>
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
        const safeAvatar = user.avatar_url ? String(user.avatar_url) : '';
        const avatar = `<div class="avatar-preview" style="flex:0 0 auto;"></div>`;
        const adminBtn = (isOwner && (isAdmin || isModerator)) ? '<button id="profile-admin" class="link-btn">Admin</button>' : '';
        header.innerHTML = `
          <div class="profile-left" style="display:flex;gap:12px;align-items:center;min-width:0;flex:1">
            ${avatar}
            <div class="profile-username" style="font-weight:700;font-size:1.1rem;font-family:var(--font-mono);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%">@${this.escapeHTML(String(user.username))}</div>
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
        this.profileTop.appendChild(header);
        // If owner and unverified, show banner with resend action
        if (isOwner && this.currentUser && this.currentUser.email_verified === false) {
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
            this.profileTop.appendChild(banner);
            const btn = banner.querySelector('#banner-resend');
            if (btn) btn.onclick = async () => {
                try {
                    const r = await fetch('/api/me/resend-verification', { method:'POST', credentials:'include' });
                    if (r.status === 204) this.showNotification('Verification sent');
                    else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Unable to send','error'); }
                } catch {}
            };
        }
        if (isOwner) {
            const sBtn = document.getElementById('profile-settings'); if (sBtn) sBtn.onclick = () => { try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.persistListState(); } catch {} history.pushState({}, '', '/settings'); this.renderSettingsPage(); };
            const aBtn = document.getElementById('profile-admin'); if (aBtn) aBtn.onclick = () => { try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.persistListState(); } catch {} history.pushState({}, '', '/admin'); this.renderAdminPage(); };
            const logoutBtn = document.getElementById('profile-logout'); if (logoutBtn) logoutBtn.onclick = async () => { await this.signOut(); window.location.href = '/'; };
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
            if (this._profileResizeHandler) {
                window.removeEventListener('resize', this._profileResizeHandler);
            }
            this._profileResizeHandler = () => applyLayout();
            window.addEventListener('resize', this._profileResizeHandler);
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
            const mSettings = document.getElementById('menu-settings'); if (mSettings) mSettings.onclick = () => { closePanel(); try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.persistListState(); } catch {} history.pushState({}, '', '/settings'); this.renderSettingsPage(); };
            const mAdmin = document.getElementById('menu-admin'); if (mAdmin) mAdmin.onclick = () => { closePanel(); try { if (location.pathname === '/' || location.pathname.startsWith('/@')) this.persistListState(); } catch {} history.pushState({}, '', '/admin'); this.renderAdminPage(); };
            const mSign = document.getElementById('menu-signout'); if (mSign) mSign.onclick = async () => { closePanel(); await this.signOut(); window.location.href = '/'; };
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
            const resp = await fetch('/api/me/profile', { method:'PATCH', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify({ bio: input.value.slice(0,500) }) });
                    if (resp.ok) { this.showNotification('Bio updated'); this.renderProfilePage(username); }
                    else { const err = await resp.json().catch(()=>({})); this.showNotification(err.error||'Save failed','error'); }
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
        this.profileTop.appendChild(tabs);

        const loadPosts = async () => {
			this.gallery.innerHTML = '';
			this.enableManagedMasonry();
            const epoch = this.renderEpoch;
            const list = imgs.images || [];
            if (this.isRestoring) {
                list.forEach((img) => this.createImageCard(img));
            } else {
                list.forEach((img, index) => {
                    const delay = this.masonry?.enabled ? Math.min(40, index * 20) : (index * 80);
                    const tid = this.trackTimeout(setTimeout(() => { this.untrackTimeout(tid); if (epoch !== this.renderEpoch) return; this.createImageCard(img); }, delay));
                });
				// Desktop fallback: if managed masonry fails to paint any cards quickly, disable it and render directly
				const failSafe = setTimeout(() => {
					if (epoch !== this.renderEpoch) return;
					const hasCards = !!this.gallery.querySelector('.image-card');
					if (!hasCards && this.masonry && this.masonry.enabled) {
						this.disableManagedMasonry();
						this.gallery.innerHTML = '';
						list.forEach((img) => this.createImageCard(img));
					}
				}, 500);
				this.trackTimeout(failSafe);
            }
        };

		const loadCollections = async () => {
			this.gallery.innerHTML = '';
			this.enableManagedMasonry();
			try {
				const resp = await fetch(`/api/users/${encodeURIComponent(username)}/collections?page=1`);
				if (!resp.ok) { this.showNotification('Failed to load collections','error'); return; }
				const data = await resp.json();
				const epoch = this.renderEpoch;
				const list = data.images || [];
				list.forEach((img, index) => {
					const delay = this.masonry?.enabled ? Math.min(40, index * 20) : (index * 80);
					const tid = this.trackTimeout(setTimeout(() => { this.untrackTimeout(tid); if (epoch !== this.renderEpoch) return; this.createImageCard(img); }, delay));
				});
				// Desktop fallback similar to posts
				const failSafe = setTimeout(() => {
					if (epoch !== this.renderEpoch) return;
					const hasCards = !!this.gallery.querySelector('.image-card');
					if (!hasCards && this.masonry && this.masonry.enabled) {
						this.disableManagedMasonry();
						this.gallery.innerHTML = '';
						list.forEach((img) => this.createImageCard(img));
					}
				}, 500);
				this.trackTimeout(failSafe);
			} catch {}
		};

        const postsBtn = tabs.querySelector('#tab-posts');
        const colBtn = tabs.querySelector('#tab-collections');
        if (postsBtn && colBtn) {
            postsBtn.onclick = async () => {
                postsBtn.setAttribute('aria-pressed','true');
                colBtn.setAttribute('aria-pressed','false');
                await loadPosts();
            };
            colBtn.onclick = async () => {
                postsBtn.setAttribute('aria-pressed','false');
                colBtn.setAttribute('aria-pressed','true');
                await loadCollections();
            };
        }

        // Default to posts; if user has no posts, show collections
        if ((imgs.images || []).length === 0) {
            postsBtn.setAttribute('aria-pressed','false');
            colBtn.setAttribute('aria-pressed','true');
            await loadCollections();
        } else {
            // Refresh my collected set so collect buttons reflect persisted state
            await this.seedMyCollectedSet();
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

    // Gallery/loading functions
    async loadImages() {
        // Only load the home feed in the feed route
        if (this.routeMode !== 'home') return;
        if (this.loading || !this.hasMore) return;
        this.loading = true;
        this.showLoader();
        try {
            const resp = await fetch(`/api/feed?page=${this.page}`, { credentials: 'include' });
            if (resp.ok) {
                const data = await resp.json();
                if (data.images && data.images.length > 0) {
					// Append to unrendered queue; do not immediately create DOM nodes for all
					this.enqueueUnrendered(data.images);
					// Attempt to reveal a small chunk if needed
					this.maybeRevealCards();
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
        const epoch = this.renderEpoch;
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
        if (this.isRestoring) {
            demoImages.forEach((image) => this.createImageCard(image));
        } else {
            demoImages.forEach((image, index) => {
                const tid = this.trackTimeout(setTimeout(() => { this.untrackTimeout(tid); if (epoch !== this.renderEpoch) return; this.createImageCard(image); }, index * 80));
            });
        }
    }

    renderImages(images) {
        // Backwards compatibility: if called directly, route through unrendered queue
        this.enqueueUnrendered(images);
        this.maybeRevealCards();
    }

    enqueueUnrendered(images) {
        if (!Array.isArray(images) || images.length === 0) return;
        for (const it of images) this.unrendered.push(it);
    }

    maybeRevealCards(maxToReveal) {
        const baseChunk = (window.innerWidth <= 600) ? 4 : 8;
        let toReveal = Math.min(baseChunk, this.unrendered.length);
        if (Number.isFinite(maxToReveal) && maxToReveal >= 0) toReveal = Math.min(toReveal, Math.max(0, Math.floor(maxToReveal)));
        if (toReveal <= 0) return 0;

        const epoch = this.renderEpoch;
        let revealed = 0;
        // On desktop with managed masonry, reveal batches as a balanced row: one per column
        const useManaged = (this.masonry && this.masonry.enabled && this.masonry.columnCount > 1 && window.innerWidth > 900);
        const batchSize = toReveal;
        for (let i = 0; i < toReveal; i++) {
            const item = this.unrendered.shift();
            if (!item) break;
            if (this.isRestoring) {
                if (useManaged) this._forcedColumnIndex = (this.masonry.appendedCount % this.masonry.columnCount);
                this.createImageCard(item);
            } else {
                // Small stagger to keep UI smooth
                const tid = this.trackTimeout(setTimeout(() => {
                    this.untrackTimeout(tid);
                    if (epoch !== this.renderEpoch) return;
                    if (useManaged) this._forcedColumnIndex = ((this.masonry.appendedCount) % this.masonry.columnCount);
                    this.createImageCard(item);
                }, Math.min(40, i * 20)));
            }
            revealed++;
        }
        return revealed;
    }

    async topUpBelowViewport(maxCycles = 3) {
        // Ensure there is scrollable content below the current viewport after restore
        const viewportH = window.innerHeight || document.documentElement.clientHeight;
        let cycles = 0;
        while (cycles < Math.max(1, maxCycles)) {
            const doc = document.documentElement;
            const bottomSpace = doc.scrollHeight - (window.scrollY + viewportH);
            if (bottomSpace >= Math.floor(viewportH * 0.75)) break;
            let revealed = 0;
            if (this.unrendered && this.unrendered.length > 0) {
                revealed = this.maybeRevealCards((window.innerWidth <= 600) ? 4 : 8);
            } else if (this.hasMore && !this.loading && this.routeMode === 'home') {
                await this.loadImages();
                revealed = this.maybeRevealCards((window.innerWidth <= 600) ? 4 : 8);
            } else {
                break;
            }
            cycles++;
            if (!revealed) break;
            // Allow layout to settle between cycles
            await new Promise(r => setTimeout(r, 16));
        }
    }

    createImageCard(image) {
        // Skip duplicates by image id if already present
        if (image && image.id) {
            const existing = this.gallery && this.gallery.querySelector(`.image-card[data-image-id="${String(image.id)}"]`);
            if (existing) return;
        }
        const card = document.createElement('div');
        card.className = 'image-card';
        card.style.animationDelay = `${Math.random() * 0.5}s`;
        if (image && image.id) { card.dataset.imageId = String(image.id); }

        const isDemo = !image.filename;
        const onProfile = location.pathname.startsWith('/@');
        const isOwner = !!this.currentUser && (this.currentUser.username === image.username);
        const isAdmin = !!this.currentUser && !!this.currentUser.is_admin;
        const isModerator = !!this.currentUser && !!this.currentUser.is_moderator;
        const canEdit = (onProfile && isOwner) || isAdmin || isModerator;
        
        let img = null; // Initialize to null for all code paths

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
            img = document.createElement('img');
            const imgURL = this.getImageURL(image.filename);
            // Defer actual src assignment to our lazy loader
            img.dataset.src = imgURL;
            img.alt = image.original_name || image.title || '';
            img.loading = 'lazy'; // native fallback
            img.style.opacity = '0.001'; // prevent layout shift flashes; cleared on load
            // Reserve space to avoid layout shifts when dimensions are known
            if (Number.isFinite(image.width) && Number.isFinite(image.height) && image.width > 0 && image.height > 0) {
                try { img.width = Math.floor(image.width); img.height = Math.floor(image.height); } catch {}
                try { img.style.aspectRatio = `${image.width} / ${image.height}`; } catch {}
            }
            img.style.background = 'var(--surface)';
            // NSFW blur logic based on current user preference
            const nsfwPref = (this.currentUser?.nsfw_pref || (this.currentUser?.show_nsfw ? 'show' : 'hide'));
            const shouldBlur = image.is_nsfw && nsfwPref === 'blur';
            const shouldHide = image.is_nsfw && (!this.currentUser || nsfwPref === 'hide');
            if (shouldHide) { 
                // Don't return early - we still need to append the card but without the image
                card.innerHTML = `
                    <div class="image-meta">
                        <div class="image-title">Content Hidden</div>
                        <div class="image-author">NSFW content filtered</div>
                    </div>`;
                this.appendCardToMasonry(card);
                return; 
            }
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
            const captionHtml = image.caption ? `<div class="image-caption" style="margin-top:4px;color:var(--text-secondary);font-size:0.8rem">${this.sanitizeAndRenderMarkdown(String(image.caption))}</div>` : '';
            const actions = canEdit ? `
                <div class="image-actions" style="display:flex;gap:2px;align-items:center;flex-shrink:0">
                  <button title="Edit" class="like-btn" data-act="edit" data-id="${image.id}" style="width:28px;height:28px;padding:0;color:var(--text-secondary)">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 1 1 3 3L7 19l-4 1 1-4Z"/></svg>
                  </button>
                  <button title="Delete" class="like-btn" data-act="delete" data-id="${image.id}" style="width:28px;height:28px;padding:0;color:#ff6666">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </div>` : '';
            // Collect button for non-owners
            const collectBtn = (!isOwner) ? `<button title="Collect" class="like-btn collect-btn${(this._myCollectedSet && this._myCollectedSet.has(String(image.id))) ? ' collected' : ''}" data-act="collect" data-id="${image.id}" style="width:32px;height:32px;padding:0;font-size:16px;opacity:0.85">${(this._myCollectedSet && this._myCollectedSet.has(String(image.id))) ? '✦' : '✧'}</button>` : '';
            meta.innerHTML = `
                <div style="display:flex;align-items:center;justify-content:space-between;gap:12px">
                  <div style="min-width:0">
                    <div class="image-title" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis"><a href="/i/${encodeURIComponent(image.id)}" class="image-link" style="color:inherit;text-decoration:none">${this.escapeHTML(String((image.title || image.original_name || 'Untitled')).trim())}</a></div>
                    <div class="image-author" style="font-family:var(--font-mono)"><a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${this.escapeHTML(String(username))}</a></div>
                  </div>
                  <div style="display:flex;gap:6px;align-items:center">${collectBtn}${actions}</div>
                </div>
                ${captionHtml}`;
            meta.addEventListener('click', async (e) => {
                const a = e.target.closest('a.image-link');
                if (a) {
                    e.preventDefault();
                    e.stopPropagation();
                    try { 
                        if (location.pathname === '/' || location.pathname.startsWith('/@')) {
                            console.log('[TROUGH] Image link click triggering persistListState');
                            this.persistListState(); 
                        }
                    } catch {}
                    history.pushState({}, '', a.getAttribute('href'));
                    await this.renderImagePage(image.id);
                    return;
                }
                const btn = e.target.closest('button');
                if (!btn) return;
                const act = btn.dataset.act;
                const id = btn.dataset.id;
                e.stopPropagation();
                if (act === 'collect') {
                    if (!this.currentUser) { this.showAuthModal(); return; }
                    btn.classList.toggle('collected');
                    btn.textContent = btn.classList.contains('collected') ? '✦' : '✧';
                    try {
                        const resp = await fetch(`/api/images/${id}/collect`, { method:'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include' });
                        if (!resp.ok) {
                            btn.classList.toggle('collected');
                            btn.textContent = btn.classList.contains('collected') ? '✦' : '✧';
                            if (resp.status === 401) { this.currentUser = null; this.checkAuth(); this.showAuthModal(); }
                            else { this.showNotification('Collect failed', 'error'); }
                        }
                        // Update in-memory cache
                        if (!this._myCollectedSet) this._myCollectedSet = new Set();
                        if (btn.classList.contains('collected')) this._myCollectedSet.add(String(id)); else this._myCollectedSet.delete(String(id));
                    } catch { btn.classList.toggle('collected'); btn.textContent = btn.classList.contains('collected') ? '✦' : '✧'; }
                } else if (act === 'delete') {
                    const ok = await this.showConfirm('Delete image?');
                    if (ok) {
                        const resp = await fetch(`/api/images/${id}`, { method: 'DELETE', credentials: 'include' });
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
                // Remove image click toggling to avoid interfering with lightbox
                // Caption can still be toggled by clicking the caption text itself below
                // Also allow toggling by clicking the caption itself
                meta.addEventListener('click', (ev) => {
                    const capEl = ev.target.closest('.image-caption');
                    if (!capEl) return;
                    // If a real link inside caption was clicked, allow navigation but prevent lightbox
                    const link = ev.target.closest('a');
                    if (link) { ev.stopPropagation(); return; }
                    toggleCaption(ev);
                });
            }
        }

        // Single vs double click: single opens lightbox; double triggers collect for non-owners
        let clickTimer = null;
        card.addEventListener('click', (e) => {
            if (e.detail === 2) return; // let dblclick handler run
            // Handle NSFW blur logic - single click removes blur immediately
            if (card.classList.contains('nsfw-blurred') && !card._nsfwRevealed) {
                // First click: reveal with animation, but do not open lightbox yet
                card._nsfwRevealed = true;
                card.classList.add('revealing');
                
                // After burning animation completes, clean up classes
                setTimeout(() => {
                    card.classList.remove('nsfw-blurred', 'revealing');
                    card.classList.add('nsfw-revealed');
                }, 1800); // Match the CSS animation duration

                e.stopPropagation();
                e.preventDefault();
                return;
            }
            
            // Delay a bit to allow dblclick to cancel
            clearTimeout(clickTimer);
            clickTimer = setTimeout(() => {
                this.openLightbox(image);
            }, 180);
        });
        card.addEventListener('dblclick', (e) => {
            clearTimeout(clickTimer);
            e.preventDefault();
            e.stopPropagation();
            if (!this.currentUser) { this.showAuthModal(); return; }
            // ignore owners
            if (this.currentUser && this.currentUser.username === image.username) return;
            const metaBtn = card.querySelector('button.collect-btn');
            if (metaBtn) this.toggleCollect(image.id, metaBtn);
            else this.toggleCollect(image.id);
        });
        this.appendCardToMasonry(card);
        
        // Register any real image with the lazy loader after it is in the DOM
        if (img && img.dataset && img.dataset.src) {
            this.registerImageForLazyLoad(img);
        }
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
              <input id="e-title" placeholder="Title" value="${this.escapeHTML(String(image.title || image.original_name || ''))}" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary)"/>
              <textarea id="e-caption" placeholder="Caption" rows="3" maxlength="2000" style="width:100%;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text-primary)">${this.escapeHTML(String(image.caption||''))}</textarea>
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
            const resp = await fetch(`/api/images/${image.id}`, { method:'PATCH', headers: { 'Content-Type': 'application/json' }, credentials: 'include', body: JSON.stringify(body) });
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
            lightboxImg.src = this.getImageURL(image.filename);
            lightboxImg.alt = image.original_name || image.title || '';
        }
        const username = image.username || image.author || 'Unknown';
        const titleText = image.title || image.original_name || 'Untitled';
        // Title becomes a link to the single-image page (escape to prevent XSS)
        lightboxTitle.innerHTML = `<a href="/i/${encodeURIComponent(image.id)}" class="image-link" style="color:inherit;text-decoration:none">${this.escapeHTML(String(titleText))}</a>`;
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
        lightboxAuthor.innerHTML = `<a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${this.escapeHTML(String(username))}</a>`;
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
            lightboxCaption.innerHTML = image.caption ? this.sanitizeAndRenderMarkdown(String(image.caption)) : '';
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
            const response = await fetch(`/api/images/${imageId}/collect`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include' });
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

    async renderSettingsPage() {
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        if (!this.currentUser) { this.showAuthModal(); return; }
        let email = '';
        try { const resp = await fetch('/api/me/account', { credentials: 'include' }); if (resp.ok) { const acc = await resp.json(); email = acc.email || ''; } } catch {}

        this.gallery.innerHTML = '';
        if (this.profileTop) this.profileTop.innerHTML = '';
        // Ensure reset page uses centered settings layout
        this.gallery.className = 'gallery settings-mode';
        this.gallery.classList.add('settings-mode');
        const wrap = document.createElement('div');
        wrap.className = 'settings-wrap';
        const avatarURL = (this.currentUser && this.currentUser.avatar_url) ? this.currentUser.avatar_url : '';
        const needVerify = !!this.currentUser && this.currentUser.email_verified === false;
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
                <input type="text" id="settings-username" value="${this.escapeHTML(String(this.currentUser.username))}" minlength="3" maxlength="30" pattern="[a-z0-9]+" title="3–30 lowercase letters or numbers" class="settings-input"/>
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
        this.gallery.appendChild(wrap);

        // Footer with public pages
        try {
            const r = await fetch('/api/pages');
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
                    a.onclick = (e) => { e.preventDefault(); history.pushState({}, '', a.href); this.init(); };
                    footer.appendChild(a);
                });
                this.gallery.appendChild(footer);
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

        // Back navigation handled centrally in setupHistoryHandler

        // Handlers remain (updated references)
        const authHeader = { 'Content-Type': 'application/json' };
        const pref = (this.currentUser?.nsfw_pref || ((this.currentUser?.show_nsfw) ? 'show' : 'hide'));
        (document.querySelector(`input[name='nsfw-pref'][value='${pref}']`)||document.querySelector(`input[name='nsfw-pref'][value='hide']`)).checked = true;
        document.getElementById('btn-nsfw').onclick = async () => {
            const sel = document.querySelector("input[name='nsfw-pref']:checked")?.value || 'hide';
            try { const resp = await fetch('/api/me/profile', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ nsfw_pref: sel }) }); if (!resp.ok) throw await resp.json(); const u = await resp.json(); this.currentUser = u; localStorage.setItem('user', JSON.stringify(u)); this.showNotification('NSFW preference saved'); } catch (e) { document.getElementById('err-nsfw').textContent = e.error || 'Failed'; }
        };
        document.getElementById('btn-username').onclick = async () => {
            const inputEl = document.getElementById('settings-username');
            const errEl = document.getElementById('err-username');
            errEl.textContent = '';
            const raw = (inputEl.value || '').trim();
            const username = raw.toLowerCase();
            const current = (this.currentUser?.username || '').toLowerCase();
            // Reserved list (mirror server)
            const RESERVED = new Set([
                'admin','administrator','adminteam','admins','root','system','sysadmin','superadmin','superuser',
                'support','help','helpdesk','moderator','mod','mods','staff','team','security','official',
                'noreply','no-reply','postmaster','abuse','report','reports','owner','undefined','null'
            ]);
            if (!username) { errEl.textContent = 'Enter a username'; return; }
            if (RESERVED.has(username)) { errEl.textContent = 'Username unavailable'; this.showNotification('Username unavailable', 'error'); return; }
            if (username === current) { errEl.textContent = 'This is already your username'; return; }
            // Preflight existence check to provide immediate feedback
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(username)}`);
                if (r && r.ok) { errEl.textContent = 'Username unavailable'; this.showNotification('Username unavailable', 'error'); return; }
            } catch {}
            try {
                const resp = await fetch('/api/me/profile', { method: 'PATCH', headers: authHeader, body: JSON.stringify({ username }) });
                if (!resp.ok) {
                    let data = {};
                    try { data = await resp.json(); } catch {}
                    const msg = (data && data.error) ? String(data.error) : '';
                    if (resp.status === 409 || (/\busername\b/i.test(msg) && /(taken|reserved|unavailable)/i.test(msg))) {
                        errEl.textContent = 'Username unavailable';
                        this.showNotification('Username unavailable', 'error');
                        return;
                    }
                    errEl.textContent = msg || 'Failed';
                    return;
                }
                const userResp = await resp.json();
                this.currentUser=userResp;
                localStorage.setItem('user', JSON.stringify(userResp));
                this.updateAuthButton();
                this.showNotification('Username changed');
            } catch (e) { errEl.textContent = e.error || 'Failed'; }
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
            try { const resp = await fetch('/api/me', { method:'DELETE', headers: authHeader, body: JSON.stringify({ confirm:'DELETE' }) }); if (resp.status !== 204) throw await resp.json(); await this.signOut(); window.location.href='/'; } catch (e) { document.getElementById('err-delete').textContent = e.error || 'Failed'; }
        };

        // Avatar upload
        document.getElementById('avatar-upload').onclick = async () => {
            const fileInput = document.getElementById('avatar-file'); const file = fileInput.files && fileInput.files[0]; if (!file) { this.showNotification('Choose a file first', 'error'); return; }
            const fd = new FormData(); fd.append('avatar', file);
            try {
                const resp = await fetch('/api/me/avatar', { method:'POST', credentials: 'include', body: fd });
                if (!resp.ok) throw await resp.json();
                const data = await resp.json();
                const pv = document.getElementById('avatar-preview'); if (pv) { try { pv.style.backgroundImage = `url('${encodeURI(String(data.avatar_url||''))}')`; } catch {} }
                this.currentUser.avatar_url = data.avatar_url; localStorage.setItem('user', JSON.stringify(this.currentUser));
                // Also update profile header avatar if present on page
                const headerAv = document.querySelector('.avatar-preview'); if (headerAv) { try { headerAv.style.backgroundImage = `url('${encodeURI(String(data.avatar_url||''))}')`; } catch {} }
                this.showNotification('Avatar updated');
            } catch (e) { this.showNotification(e.error || 'Upload failed', 'error'); }
        };
        if (needVerify) {
            const btn1 = document.getElementById('btn-resend-verify');
            if (btn1) btn1.onclick = async () => {
                try { const r = await fetch('/api/me/resend-verification', { method:'POST', credentials:'include' }); if (r.status===204) this.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Unable to send','error'); } } catch {}
            };
            const btn2 = document.getElementById('settings-resend-verify');
            if (btn2) btn2.onclick = async () => {
                try { const r = await fetch('/api/me/resend-verification', { method:'POST', credentials:'include' }); if (r.status===204) this.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Unable to send','error'); } } catch {}
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
            const response = await fetch('/api/upload', { method: 'POST', credentials: 'include', body: formData });
            
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
        // Ensure only one scroll listener is attached at a time
        if (this._infiniteScrollCleanup) { try { this._infiniteScrollCleanup(); } catch {} this._infiniteScrollCleanup = null; }
        // Prefer an IntersectionObserver sentinel to avoid premature loads on mobile
        const sentinel = document.createElement('div');
        sentinel.id = 'infinite-scroll-sentinel';
        sentinel.style.cssText = 'width:1px;height:1px;';
        // Ensure sentinel is appended at the end of the gallery (after any unrendered reveals too)
        const appendSentinel = () => {
            try {
                if (!this.gallery) return;
                if (!document.getElementById('infinite-scroll-sentinel')) {
                    this.gallery.appendChild(sentinel);
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
					const revealed = this.maybeRevealCards(maxReveal);
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
					const doc = document.documentElement;
					const needsFill = (doc.scrollHeight <= (window.innerHeight + 200));
					if (!this._userInteracted) {
						if (this._autoFillDone || !needsFill) continue;
						this._autoFillDone = true;
					}

					this._infinitePendingExit = true;
					this.loadImages();
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
                            this.loadImages();
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

    // Managed masonry: explicit column containers for efficient initial load
    enableManagedMasonry() {
        const g = this.gallery;
        if (!g) return;
        // On mobile, keep single column and native flow
        const mobile = window.innerWidth <= 600;
        if (mobile) { this.disableManagedMasonry(); return; }
        // Compute column count based on viewport
        let cols = 4;
        if (window.innerWidth <= 1400) cols = 3;
        if (window.innerWidth <= 900) cols = 2;
        if (window.innerWidth <= 600) cols = 1;
        // If already enabled and column count unchanged, do nothing
        if (this.masonry.enabled && this.masonry.columnCount === cols && g.classList.contains('masonry-managed')) {
            return;
        }
        // Collect existing cards regardless of current structure
        const existingCards = Array.from(g.querySelectorAll('.image-card'));
        g.classList.add('masonry-managed');
        g.style.setProperty('--masonry-cols', String(cols));
        // Initialize masonry state
        this.masonry = { enabled: true, columnCount: cols, columns: [], nextColumnIndex: 0, appendedCount: 0 };
        // Rebuild columns
        const fragment = document.createDocumentFragment();
        for (let i = 0; i < cols; i++) {
            const col = document.createElement('div');
            col.className = 'masonry-col';
            fragment.appendChild(col);
            this.masonry.columns.push(col);
        }
        g.innerHTML = '';
        g.appendChild(fragment);
        // Re-append any existing cards using smart placement
        existingCards.forEach((card) => {
            this.placeCardSmart(card);
        });
        // Reset nextColumnIndex to the next column after a full first row fill
        this.masonry.nextColumnIndex = this.masonry.appendedCount % this.masonry.columnCount;
        // Handle resize to adjust columns (debounced)
        if (this._masonryResizeHandler) window.removeEventListener('resize', this._masonryResizeHandler);
        this._masonryResizeHandler = () => {
            if (this._masonryResizeTimer) { clearTimeout(this._masonryResizeTimer); this._masonryResizeTimer = null; }
            this._masonryResizeTimer = setTimeout(() => {
                const prevCols = this.masonry.columnCount;
                let newCols = 4;
                if (window.innerWidth <= 1400) newCols = 3;
                if (window.innerWidth <= 900) newCols = 2;
                if (window.innerWidth <= 600) newCols = 1;
                if (newCols !== prevCols) {
                    this.enableManagedMasonry();
                }
            }, 120);
        };
        window.addEventListener('resize', this._masonryResizeHandler);
    }

    disableManagedMasonry() {
        const g = this.gallery;
        if (!g) return;
        if (!this.masonry?.enabled) return;
        // Move children cards back to gallery root in visual order
        const cards = [];
        for (const col of this.masonry.columns || []) {
            cards.push(...Array.from(col.children));
        }
        g.classList.remove('masonry-managed');
        if (this._masonryResizeHandler) { window.removeEventListener('resize', this._masonryResizeHandler); this._masonryResizeHandler = null; }
        g.innerHTML = '';
        for (const c of cards) g.appendChild(c);
        this.masonry = { enabled: false, columnCount: 0, columns: [], nextColumnIndex: 0, appendedCount: 0 };
    }

    appendCardToMasonry(card) {
        if (this.masonry && this.masonry.enabled && this.masonry.columns?.length > 0) {
            // If a forced column is specified (balanced reveal), honor it
            if (Number.isInteger(this._forcedColumnIndex) && this._forcedColumnIndex >= 0 && this._forcedColumnIndex < this.masonry.columnCount) {
                const idx = this._forcedColumnIndex;
                this._forcedColumnIndex = null;
                this.masonry.columns[idx].appendChild(card);
                this.masonry.appendedCount = (this.masonry.appendedCount || 0) + 1;
                // Keep nextColumnIndex in sync with round-robin expectation
                this.masonry.nextColumnIndex = (idx + 1) % this.masonry.columnCount;
            } else {
                this.placeCardSmart(card);
            }
            // Update row-height hint from the first row for smarter buffer sizing
            if (this.masonry.appendedCount <= this.masonry.columnCount) {
                const cr = card.getBoundingClientRect();
                if (cr && cr.height && Number.isFinite(cr.height)) {
                    this.masonry.rowHeightHint = Math.max(this.masonry.rowHeightHint || 0, cr.height);
                }
            }
        } else {
            this.gallery.appendChild(card);
        }
        // Keep sentinel at the end so IO triggers correctly
        try {
            const sentinel = document.getElementById('infinite-scroll-sentinel');
            if (sentinel && sentinel.parentNode === this.gallery) {
                this.gallery.appendChild(sentinel);
            }
        } catch {}
    }

    // Place first row round-robin, then append to shortest column for balance
    placeCardSmart(card) {
        // Assign sequential index for cheap eager check
        if (this.masonry.appendedCount == null) this.masonry.appendedCount = 0;
        const seq = this.masonry.appendedCount;
        try { card.dataset.seqIndex = String(seq); } catch {}
        this.masonry.appendedCount++;
        // First row: one per column left->right
        if (seq < this.masonry.columnCount) {
            const idx = this.masonry.nextColumnIndex % this.masonry.columnCount;
            this.masonry.columns[idx].appendChild(card);
            this.masonry.nextColumnIndex = (idx + 1) % this.masonry.columnCount;
            return;
        }
        // After first row: choose the shortest column (tie-breaker: round-robin)
        let minIdx = 0;
        let minH = Number.POSITIVE_INFINITY;
        let maxH = 0;
        for (let i = 0; i < this.masonry.columns.length; i++) {
            const h = this.masonry.columns[i].offsetHeight || 0;
            if (h < minH) { minH = h; minIdx = i; }
            if (h > maxH) { maxH = h; }
        }
        const nearlyEqual = (maxH - minH) <= 2; // px tolerance when images not yet loaded
        if (nearlyEqual) {
            const idx = this.masonry.nextColumnIndex % this.masonry.columnCount;
            this.masonry.columns[idx].appendChild(card);
            this.masonry.nextColumnIndex = (idx + 1) % this.masonry.columnCount;
        } else {
            this.masonry.columns[minIdx].appendChild(card);
        }
    }

	// Compute lazy buffer height: aim for ~two rows in managed mode, otherwise fallback
    computeLazyBuffer() {
		if (this.masonry && this.masonry.enabled) {
            const rowH = this.masonry.rowHeightHint || 400;
            // Two rows plus some breathing room
            return Math.min(1600, Math.max(600, Math.round(rowH * 2.25)));
        }
		// On mobile, use a smaller buffer to avoid over-eager loading
		return (window.innerWidth <= 600) ? 250 : 600;
    }

    // (legacy mobile magnetic snap system removed)

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
            try { const r = await fetch('/api/admin/site', { credentials: 'include' }); if (r.ok) s = await r.json(); } catch {}
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
            // Event handlers will be wired up in the isAdmin block below

            // Storage test handler will be wired up in the isAdmin block below

            const favInput = document.getElementById('favicon-file');
            const favPreview = document.getElementById('favicon-preview');
            if (favInput) favInput.onchange = () => { const f = favInput.files && favInput.files[0]; if (f) { favPreview.src = URL.createObjectURL(f); favPreview.style.display='inline-block'; } };

            const upFavBtn = document.getElementById('btn-upload-favicon');
            if (upFavBtn) upFavBtn.onclick = async () => {
                const f = favInput.files[0]; if (!f) { this.showNotification('Choose a favicon file', 'error'); return; }
                const fd = new FormData(); fd.append('favicon', f);
                const r = await fetch('/api/admin/site/favicon', { method:'POST', credentials:'include', body: fd });
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
                const r = await fetch('/api/admin/site/social-image', { method:'POST', credentials:'include', body: fd });
                if (r.ok) { const d = await r.json(); document.getElementById('social-image').value = d.social_image_url || ''; socialPreview.src = d.social_image_url || socialPreview.src; socialPreview.style.display='inline-block'; this.showNotification('Social image uploaded'); }
                else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Upload failed','error'); }
            };

            // Analytics dynamic UI (bind before attachment by scoping to siteSection)
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
        // Build tabs
        const tabsWrap = document.createElement('div');
        tabsWrap.className = 'tab-group';
        tabsWrap.style.cssText = 'margin:0 auto 12px;display:flex;gap:8px;flex-wrap:wrap;';
        const mkTab = (id, label) => { const b = document.createElement('button'); b.className='tab-btn'; b.dataset.tab=id; b.textContent=label; return b; };
        const tabSite = mkTab('site', 'Site settings');
        const tabPages = isAdmin ? mkTab('pages', 'Add/Edit Pages') : null;
        const tabInv = mkTab('invites', 'Invitations');
        const tabUsers = mkTab('users', 'User management');
        tabsWrap.appendChild(tabSite);
        if (tabPages) tabsWrap.appendChild(tabPages);
        tabsWrap.appendChild(tabInv);
        tabsWrap.appendChild(tabUsers);
        wrap.appendChild(tabsWrap);
        // Sections container
        const sections = document.createElement('div');
        sections.appendChild(siteSection);
        if (isAdmin) sections.appendChild(pagesSection);
        sections.appendChild(invitesSection);
        sections.appendChild(usersSection);
        wrap.appendChild(sections);
        const showSection = (name) => {
            const map = { site: siteSection, pages: pagesSection, invites: invitesSection, users: usersSection };
            [siteSection, pagesSection, invitesSection, usersSection].forEach(sec => { if (sec) sec.style.display = 'none'; });
            if (map[name]) map[name].style.display = 'block';
            const setActive = (btn, on) => { if (!btn) return; if (on) btn.classList.add('active'); else btn.classList.remove('active'); };
            setActive(tabSite, name==='site'); setActive(tabPages, name==='pages'); setActive(tabInv, name==='invites'); setActive(tabUsers, name==='users');
        };
        // Default tab
        showSection('site');
        // Wire tab clicks
        tabSite.onclick = () => showSection('site');
        if (tabPages) tabPages.onclick = () => showSection('pages');
        tabInv.onclick = () => showSection('invites');
        tabUsers.onclick = () => showSection('users');
        
        this.gallery.appendChild(wrap);

        if (isAdmin) {
            // Pages management
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
                const r = await fetch(`/api/admin/pages?page=${page}&limit=200`, { credentials:'include' });
                if (!r.ok) { this.showNotification('Failed to load pages','error'); return; }
                const d = await r.json().catch(()=>({pages:[]}));
                pgList.innerHTML = '';
                (d.pages||[]).forEach(p => {
                    const row = document.createElement('div');
                    row.style.cssText = 'display:grid;grid-template-columns:1fr auto auto;gap:8px;align-items:center;border:1px solid var(--border);border-radius:8px;padding:8px;';
                    row.innerHTML = `<div><div style="font-weight:600">${this.escapeHTML(String(p.title||''))}</div><div class="meta" style="opacity:.8">/${this.escapeHTML(String(p.slug||''))} ${p.is_published?'• Published':''}</div></div><button class="nav-btn" data-act="edit">Edit</button><button class="nav-btn nav-btn-danger" data-act="remove">Delete</button>`;
                    row.querySelector('[data-act="edit"]').onclick = () => {
                        selectedId = p.id; pgSlug.value = p.slug||''; pgTitle.value = p.title||''; pgMarkdown.value = p.markdown||''; pgRedirect.value = p.redirect_url||''; pgMetaTitle.value = p.meta_title||''; pgMetaDesc.value = p.meta_description||''; pgPub.checked = !!p.is_published;
                    };
                    row.querySelector('[data-act="remove"]').onclick = async () => {
                        const ok = await this.showConfirm('Delete this page?'); if (!ok) return;
                        const rr = await fetch(`/api/admin/pages/${encodeURIComponent(p.id)}`, { method:'DELETE', credentials:'include' });
                        if (rr.status===204) { this.showNotification('Deleted'); loadPages(1); if (selectedId===p.id) { selectedId=null; pgNew.click(); } }
                        else { const e = await rr.json().catch(()=>({})); this.showNotification(e.error||'Delete failed','error'); }
                    };
                    pgList.appendChild(row);
                });
            };
            pgNew.onclick = () => { selectedId = null; pgSlug.value=''; pgTitle.value=''; pgRedirect.value=''; pgMarkdown.value=''; pgMetaTitle.value=''; pgMetaDesc.value=''; pgPub.checked=false; };
            pgDel.onclick = async () => { if (!selectedId) return; const ok = await this.showConfirm('Delete this page?'); if (!ok) return; const r = await fetch(`/api/admin/pages/${encodeURIComponent(selectedId)}`, { method:'DELETE', credentials:'include' }); if (r.status===204) { this.showNotification('Deleted'); selectedId=null; pgNew.click(); loadPages(1); } else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Delete failed','error'); } };
            pgSave.onclick = async () => {
                const slug = (pgSlug.value||'').trim().toLowerCase();
                if (!slugRe.test(slug)) { this.showNotification('Invalid slug','error'); return; }
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
                const url = selectedId ? `/api/admin/pages/${encodeURIComponent(selectedId)}` : '/api/admin/pages';
                const r = await fetch(url, { method, headers:{'Content-Type':'application/json'}, credentials:'include', body: JSON.stringify(body) });
                if (r.ok || r.status===201) { this.showNotification('Saved'); loadPages(1); }
                else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Save failed','error'); }
            };
            await loadPages(1);
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
                    public_registration_enabled: document.getElementById('public-reg')?.checked !== false,
                    analytics_enabled: document.getElementById('analytics-enabled')?.checked || false,
                    analytics_provider: document.getElementById('analytics-provider')?.value || '',
                    ga4_measurement_id: document.getElementById('ga4-id')?.value || '',
                    umami_src: document.getElementById('umami-src')?.value || '',
                    umami_website_id: document.getElementById('umami-website-id')?.value || '',
                    plausible_src: document.getElementById('plausible-src')?.value || '',
                    plausible_domain: document.getElementById('plausible-domain')?.value || '',
                };
                const r = await fetch('/api/admin/site', { method:'PUT', headers: { 'Content-Type':'application/json' }, credentials: 'include', body: JSON.stringify(body) });
                if (r.ok) { this.showNotification('Saved'); await this.applyPublicSiteSettings(); }
                else { this.showNotification('Save failed','error'); }
            };
            // Toggle advanced sections visibility
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

            // Hide/show storage advanced based on provider
            const providerSel = document.getElementById('storage-provider');
            const s3Adv = document.getElementById('s3-advanced');
            if (providerSel && s3Adv) {
                providerSel.onchange = () => {
                    const v = providerSel.value;
                    s3Adv.style.display = (v === 's3' || v === 'r2') ? 'grid' : 'none';
                };
            }

            // Invites management
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
                try { await navigator.clipboard.writeText(text); this.showNotification('Copied'); } catch { this.showNotification('Copy failed','error'); }
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
                            <div class="code">${this.escapeHTML(String(inv.code))}</div>
                            <div class="invite-meta">Uses: ${this.escapeHTML(String(usesStr))} • Expires: ${this.escapeHTML(String(expStr))}</div>
                          </div>
                          <div class="invite-actions">
                            <button class="nav-btn" data-act="copy">Copy link</button>
                            <button class="nav-btn" data-act="copy-code">Copy code</button>
                            <button class="nav-btn nav-btn-danger" data-act="revoke">Revoke</button>
                          </div>`;
                        row.querySelector('[data-act="copy"]').onclick = () => copyToClipboard(buildLink(inv.code));
                        row.querySelector('[data-act="copy-code"]').onclick = () => copyToClipboard(inv.code);
                        row.querySelector('[data-act="revoke"]').onclick = async () => {
                            const ok = await this.showConfirm('Revoke this invite?'); if (!ok) return;
                const r = await fetch(`/api/admin/invites/${inv.id}`, { method:'DELETE', credentials:'include' });
                            if (r.status === 204) { this.showNotification('Invite revoked'); await loadInvites(invPage); }
                            else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Revoke failed','error'); }
                        };
                        invList.appendChild(row);
                    });
                    // Add prune link when invites exist
                    const prune = document.createElement('div');
                    prune.className = 'invite-prune';
                    prune.innerHTML = '<button id="inv-prune" class="link-btn">Clear Used and Expired codes</button>';
                    invList.appendChild(prune);
                    const pruneBtn = document.getElementById('inv-prune');
                    if (pruneBtn) pruneBtn.onclick = async (e) => {
                        e.preventDefault();
                        const ok = await this.showConfirm('Clear all used and expired invites?');
                        if (!ok) return;
                        const r = await fetch('/api/admin/invites/prune', { method:'POST', credentials:'include' });
                        if (r.ok) { this.showNotification('Cleared'); await loadInvites(invPage); }
                        else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Failed to clear','error'); }
                    };
                }
                const totalPages = Math.max(1, Math.ceil(total/limit));
                if (invInfo) invInfo.textContent = `Page ${page} of ${totalPages} • ${total} total`;
                if (invPrev) invPrev.disabled = page <= 1;
                if (invNext) invNext.disabled = page >= totalPages;
            };
            const loadInvites = async (page=1) => {
                invPage = page;
                const r = await fetch(`/api/admin/invites?page=${page}&limit=${invLimit}`, { credentials:'include' });
                if (!r.ok) { this.showNotification('Failed to load invites','error'); return; }
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
                const r = await fetch('/api/admin/invites', { method:'POST', headers:{ 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify(body) });
                if (r.ok || r.status === 201) {
                    const d = await r.json().catch(()=>({}));
                    this.showNotification('Invite created');
                    if (d.link) { try { await navigator.clipboard.writeText(d.link); this.showNotification('Link copied'); } catch {} }
                    await loadInvites(1);
                } else {
                    const e = await r.json().catch(()=>({}));
                    this.showNotification(e.error||'Create failed','error');
                }
            };
            await loadInvites(1);

            // Wire up all event handlers
            const saveBtnTop = document.getElementById('btn-save-site-top');
            if (saveBtnTop) saveBtnTop.onclick = doSave;
            const saveBtn = document.getElementById('btn-save-site');
            if (saveBtn) saveBtn.onclick = doSave;
            const saveCore = document.getElementById('btn-save-site-core');
            if (saveCore) saveCore.onclick = doSave;

            // Wire SMTP test
            const btnTest = document.getElementById('btn-smtp-test');
            if (btnTest) btnTest.onclick = async () => {
                const to = (document.getElementById('smtp-test-to').value||'').trim();
                if(!to){ this.showNotification('Enter recipient','error'); return;}
                const r = await fetch('/api/admin/site/test-smtp', { method:'POST', headers:{ 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ to }) });
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
                const r = await fetch('/api/admin/site/test-storage', { method:'POST', credentials:'include' });
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
            const saveStorageBtn = document.getElementById('btn-save-storage');
            if (saveStorageBtn) saveStorageBtn.onclick = doSave;
        }

        // User search (kept) + actions per result
        const isAdminLocal = isAdmin; // capture for closures
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
                left.innerHTML = `<div class="handle">@${this.escapeHTML(String(u.username))}</div><div class="id">${this.escapeHTML(String(u.id))}</div>`;
                const right = document.createElement('div'); right.className='actions';
                const modBtn = document.createElement('button'); modBtn.className='nav-btn'; modBtn.textContent = u.is_moderator ? 'Unmod' : 'Make mod';
                modBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ is_moderator: !u.is_moderator }) }); if (r.ok) { u.is_moderator = !u.is_moderator; modBtn.textContent = u.is_moderator ? 'Unmod' : 'Make mod'; } };
                right.appendChild(modBtn);
                if (isAdminLocal) {
                    const adminBtn = document.createElement('button'); adminBtn.className='nav-btn'; adminBtn.textContent = u.is_admin ? 'Revoke admin' : 'Make admin';
                    adminBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ is_admin: !u.is_admin }) }); if (r.ok) { u.is_admin = !u.is_admin; adminBtn.textContent = u.is_admin ? 'Revoke admin' : 'Make admin'; } };
                    right.appendChild(adminBtn);
                    const disableBtn = document.createElement('button'); disableBtn.className='nav-btn'; disableBtn.textContent = u.is_disabled ? 'Enable' : 'Disable';
                    disableBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}`, { method:'PATCH', headers: { 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify({ is_disabled: !u.is_disabled }) }); if (r.ok) { u.is_disabled = !u.is_disabled; disableBtn.textContent = u.is_disabled ? 'Enable' : 'Disable'; } };
                    right.appendChild(disableBtn);
                    const verifyBtn = document.createElement('button'); verifyBtn.className='nav-btn'; verifyBtn.textContent='Send verify';
                    verifyBtn.onclick = async () => { const r = await fetch(`/api/admin/users/${u.id}/send-verification`, { method:'POST', credentials:'include' }); if (r.status===204) this.showNotification('Verification sent'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Failed','error'); } };
                    right.appendChild(verifyBtn);
                    const delBtn = document.createElement('button'); delBtn.className='nav-btn'; delBtn.style.background='var(--color-danger)'; delBtn.style.color='#fff'; delBtn.textContent='Delete';
                    delBtn.onclick = async () => { const ok = await this.showConfirm('Delete user?'); if (!ok) return; const r = await fetch(`/api/admin/users/${u.id}`, { method:'DELETE', credentials:'include' }); if (r.status===204) { row.remove(); } else { this.showNotification('Delete failed','error'); } };
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
            const r = await fetch(`/api/admin/users?q=${encodeURIComponent(q)}&page=${page}&limit=${pageSize}`, { credentials: 'include' });
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
        if (!token) { this.showNotification('Invalid reset link','error'); history.replaceState({}, '', '/'); await this.init(); return; }
        this.gallery.innerHTML = '';
        // Ensure reset page uses centered settings layout
        if (this.gallery) {
            this.gallery.className = 'gallery settings-mode';
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
        this.gallery.appendChild(wrap);
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
            const r = await fetch('/api/reset-password', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ token, new_password:a }) });
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
        try { const r = await fetch('/api/verify-email', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ token }) }); if (r.status===204) this.showNotification('Email verified'); else { const e = await r.json().catch(()=>({})); this.showNotification(e.error||'Verification failed','error'); } } catch {}
        history.replaceState({}, '', '/'); this.init();
    }

    async openForgotPassword() {
        const overlay = document.createElement('div'); overlay.style.cssText='position:fixed;inset:0;z-index:3050;background:rgba(0,0,0,0.6);backdrop-filter:blur(8px);display:flex;align-items:center;justify-content:center;padding:24px;';
        const panel = document.createElement('div'); panel.style.cssText='max-width:420px;width:100%;background:var(--surface-elevated);border:1px solid var(--border);border-radius:12px;padding:16px;color:var(--text-primary)';
        panel.innerHTML = `<div class="settings-label">Reset your password</div><input id="fp-email" type="email" class="settings-input" placeholder="Email"/><div class="settings-actions"><button id="fp-send" class="nav-btn">Send reset link</button></div>`;
        overlay.appendChild(panel); document.body.appendChild(overlay);
        overlay.addEventListener('click', (e)=>{ if(e.target===overlay) overlay.remove(); });
        panel.querySelector('#fp-send').onclick = async () => {
            const email = panel.querySelector('#fp-email').value.trim();
            if (!email) { this.showNotification('Enter your email','error'); return; }
            const btn = panel.querySelector('#fp-send');
            btn.disabled = true;
            const prevText = btn.textContent;
            btn.textContent = 'Sending…';
            try {
                const r = await fetch('/api/forgot-password', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ email }) });
                if (r.status===204) {
                    this.showNotification('Check your email');
                    overlay.remove();
                } else {
                    const e = await r.json().catch(()=>({}));
                    this.showNotification(e.error||'Unable to send','error');
                    btn.disabled = false;
                    btn.textContent = prevText;
                }
            } catch {
                this.showNotification('Unable to send','error');
                btn.disabled = false;
                btn.textContent = prevText;
            }
        };
    }

    async renderImagePage(id) {
        if (this.magneticScroll && this.magneticScroll.updateEnabledState) this.magneticScroll.updateEnabledState();
        if (this.profileTop) this.profileTop.innerHTML = '';
        this.gallery.innerHTML = '';
        this.gallery.classList.add('settings-mode');
        // Ensure my collected set is hydrated for button state
        await this.seedMyCollectedSet();
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
        const captionHtml = data.caption ? `<div class="image-caption" id="single-caption" style="margin-top:8px;color:var(--text-secondary);position:relative">${this.sanitizeAndRenderMarkdown(String(data.caption))}</div>` : '';
        wrap.innerHTML = `
          <div style="display:grid;gap:12px">
            <div class="single-header">
              <h1 class="single-title" title="${this.escapeHTML(String(title))}">${this.escapeHTML(String(title))}</h1>
              <a href="/@${encodeURIComponent(username)}" class="single-username link-btn" style="text-decoration:none">@${this.escapeHTML(String(username))}</a>
            </div>
            <div style="position:relative;display:flex;justify-content:center">
              <img src="${this.getImageURL(data.filename)}" alt="${title}" style="max-width:100%;max-height:76vh;border-radius:10px;border:1px solid var(--border)"/>
              <button id="single-collect" class="like-btn collect-btn" title="Collect" style="position:absolute;right:10px;bottom:10px;width:44px;height:44px;font-size:18px;backdrop-filter:blur(6px)">✧</button>
            </div>
            ${captionHtml}
          </div>`;
        this.gallery.appendChild(wrap);

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

            const imgURL = this.getImageURL(data.filename);
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
            const r = await fetch(`/api/pages/${encodeURIComponent(slug)}`);
            if (!r.ok) return false;
            const d = await r.json().catch(()=>null);
            if (!d) return false;
            if (d.redirect_url) { window.location.href = d.redirect_url; return true; }
            if (this.profileTop) this.profileTop.innerHTML = '';
            this.gallery.innerHTML = '';
            this.gallery.classList.add('settings-mode');
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
            this.gallery.appendChild(wrap);
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
                                        const toc = '<nav class="page-toc"><ul>' + headings.map(h=>`<li class="lv${h.level}"><a href="#${h.slug}">${this.escapeHTML(String(h.text||''))}</a></li>`).join('') + '</ul></nav>';
                                        src = src.replace('[[TOC]]', toc);
                                    }
                                } catch {}
                            }
                            html = md.render(src);
                        } else if (window.marked) {
                            if (window.marked.setOptions) { window.marked.setOptions({ gfm: true, breaks: true }); }
                            html = window.marked.parse(raw);
                        } else {
                            html = this.sanitizeAndRenderMarkdown(raw);
                        }
                    } catch { html = this.sanitizeAndRenderMarkdown(raw); }
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
                        const rr = await fetch('/api/admin/pages?limit=200', { credentials: 'include' });
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
                          <div style="display:grid;gap:6px"><label class="settings-label">Slug</label><input id="pgx-slug" class="settings-input" disabled value="${this.escapeHTML(String(slug))}"/></div>
                          <div style="display:grid;gap:6px"><label class="settings-label">Title</label><input id="pgx-title" class="settings-input" value="${this.escapeHTML(String(pageRow?.title||''))}"/></div>
                          <div style="display:grid;gap:6px"><label class="settings-label">Redirect URL</label><input id="pgx-redirect" class="settings-input" value="${this.escapeHTML(String(pageRow?.redirect_url||''))}" placeholder="https://..."/></div>
                        </div>
                        <div style="display:grid;gap:6px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr))">
                          <div style="display:grid;gap:6px"><label class="settings-label">Meta title</label><input id="pgx-meta-title" class="settings-input" value="${this.escapeHTML(String(d.meta_title||''))}"/></div>
                          <div style="display:grid;gap:6px"><label class="settings-label">Meta description</label><input id="pgx-meta-desc" class="settings-input" value="${this.escapeHTML(String(d.meta_description||''))}"/></div>
                        </div>
                        <label style="display:flex;gap:8px;align-items:center"><input id="pgx-pub" type="checkbox" ${pageRow?.is_published? 'checked':''}/> Published</label>
                        <div style="display:grid;gap:6px"><label class="settings-label">Content (Markdown)</label><textarea id="pgx-md" class="settings-input" style="min-height:320px">${this.escapeHTML(String(d.markdown||''))}</textarea></div>
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
                            const rr = await fetch(`/api/admin/pages/${encodeURIComponent(pid)}`, { method:'PUT', headers:{ 'Content-Type':'application/json' }, credentials:'include', body: JSON.stringify(body) });
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
            // Base debounce before attempting drift (ms). Actual delay becomes dynamic.
            settleDelay: 280,
            driftCheckInterval: 16,
            maxDriftSpeed: 0.8,
            minDriftSpeed: 0.05,
            attractionStrength: 0.015,
            damping: 0.92,
            effectiveRange: 200,
            // Allow attraction to the first and second posts as well
            minCardIndex: 0,
            captionMaxHeight: 0.4,
            velocityMemory: 5,
            highVelocityThreshold: 8,
            // New tuning levers for user-friendliness
            idleBeforeDriftMs: 280,              // required quiet time after last input
            lowVelocityThreshold: 0.06,          // px/ms; must be below to engage
            mediumVelocityThreshold: 0.2,        // px/ms; affects settle delay
            wheelSettleMs: 450,                  // ms after wheel input
            minDistanceToDrift: 18,              // px minimum distance to bother drifting
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
            driftAnimationId: null,
            lastUserInputTime: 0
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
        this.handleKeydown = this.handleKeydown.bind(this);
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
        window.addEventListener('keydown', this.handleKeydown, { passive: true });
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
        window.removeEventListener('keydown', this.handleKeydown);
    }

    updateEnabledState() {
        const isMobileWidth = window.matchMedia('(max-width: 768px)').matches;
        const hasTouch = ('ontouchstart' in window) || (navigator.maxTouchPoints > 0) || window.matchMedia('(pointer: coarse)').matches;
        const hasModalOpen = document.body.style.overflow === 'hidden';
        const isSpecialPage = /^\/(settings|admin|reset|verify)/.test(location.pathname);
        const isListPage = (location.pathname === '/' || location.pathname.startsWith('/@'));
        // Force enable on main feed and profile pages when conditions allow (mobile + touch + no modal)
        this.state.enabled = isMobileWidth && hasTouch && !hasModalOpen && !isSpecialPage && isListPage;
        
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
        this.state.lastUserInputTime = now;
        this.scheduleSettle();
    }

    handleTouchStart(e) {
        if (!this.state.enabled) return;
        const t = e.touches[0]; if (!t) return;
        this.touch = { active: true, startY: t.clientY, startTime: performance.now(), velocities: [] };
        this.stopDrift();
        this.state.lastUserInputTime = performance.now();
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
        this.state.lastUserInputTime = now;
    }

    handleTouchEnd() {
        if (!this.state.enabled || !this.touch.active) return;
        this.touch.active = false;
        this.state.lastUserInputTime = performance.now();
        if (this.touch.velocities.length > 0) {
            const avg = this.touch.velocities.reduce((a,b)=>a+b,0) / this.touch.velocities.length;
            // Convert px/frame to px/ms (assuming 60fps = 16.67ms per frame)
            const threshold = this.config.highVelocityThreshold / 16.67;
            // console.log('MagneticScroll.handleTouchEnd velocity check:', { avg, threshold, velocities: this.touch.velocities });
            if (Math.abs(avg) > threshold) this.scheduleSettle(this.config.wheelSettleMs);
            else this.scheduleSettle(this.config.settleDelay);
        } else {
            this.scheduleSettle(this.config.settleDelay);
        }
    }

    handleWheel() {
        if (!this.state.enabled) return;
        this.stopDrift();
        this.state.lastUserInputTime = performance.now();
        this.scheduleSettle(this.config.wheelSettleMs);
    }

    handleKeydown(e) {
        if (!this.state.enabled) return;
        // Any keyboard input should immediately cancel drift for accessibility
        this.stopDrift();
        clearTimeout(this.state.settleTimer);
        // Give user a moment; then allow a gentle settle if they stop interacting
        this.scheduleSettle(300);
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
        const now = performance.now();
        const timeSinceInput = now - this.state.lastUserInputTime;
        // Dynamic settle based on average user velocity and idle time
        const avgV = Math.abs(this.getAverageVelocity());
        let baseDelay = delay ?? this.config.settleDelay;
        if (avgV > this.config.mediumVelocityThreshold) baseDelay = Math.max(baseDelay, this.config.wheelSettleMs);
        else if (avgV > this.config.lowVelocityThreshold) baseDelay = Math.max(baseDelay, this.config.idleBeforeDriftMs);
        const minIdle = delay ?? this.config.idleBeforeDriftMs;
        const remainingIdle = Math.max(0, minIdle - timeSinceInput);
        const finalDelay = Math.max(remainingIdle, baseDelay);
        this.state.settleTimer = setTimeout(() => this.beginDrift(), finalDelay);
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
        
        // Accessibility: if user has prefers-reduced-motion, avoid auto-drift entirely
        if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

        // If we're at the very top, do not skip initial cards/profile header
        if (window.scrollY < 24) return;

        // Do not fight the user: if average velocity recently indicates medium movement, skip drift
        const avgV = Math.abs(this.getAverageVelocity());
        if (avgV > this.config.mediumVelocityThreshold) return;

        const point = this.findAttractionPoint();
        // console.log('MagneticScroll.beginDrift attraction point:', point);
        if (!point) return;
        const currentY = window.scrollY; const distance = Math.abs(point.position - currentY);
        // console.log('MagneticScroll.beginDrift distance check:', { currentY, targetY: point.position, distance });
        if (distance < this.config.minDistanceToDrift) return; // avoid micro-adjustments
        this.state.targetPosition = point.position;
        // Start with a small initial velocity toward the target to create an ease-in feel
        const direction = Math.sign(point.position - currentY) || 1;
        this.state.currentVelocity = direction * Math.max(this.config.minDriftSpeed * 1.5, 0.08);
        this.state.isDrifting = true;
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
            this.smoothScrollTo(targetY, 220);
            this.stopDrift(); 
            return; 
        }
        
        // Dynamic attraction force - stronger when closer, with smooth falloff
        const normalizedDistance = Math.min(1, Math.abs(distance) / this.config.effectiveRange);
        // Use a gentle S-curve so force ramps in smoothly and never spikes
        const attractionCurve = 0.5 - 0.5 * Math.cos(Math.PI * (1 - normalizedDistance));
        const baseForce = this.config.attractionStrength * attractionCurve;
        
        // Prevent overshoot by reducing force when velocity is in same direction as distance
        const velocityDirection = Math.sign(this.state.currentVelocity);
        const distanceDirection = Math.sign(distance);
        const overshootDamping = (velocityDirection === distanceDirection && Math.abs(this.state.currentVelocity) > 0.35) ? 0.55 : 1;
        
        const force = distanceDirection * baseForce * overshootDamping * Math.min(Math.abs(distance), 30);
        
        // Apply force and damping
        this.state.currentVelocity += force;
        this.state.currentVelocity *= this.config.damping;
        
        // Clamp velocity
        this.state.currentVelocity = Math.max(-this.config.maxDriftSpeed, Math.min(this.config.maxDriftSpeed, this.state.currentVelocity));
        
        // Smooth stop when velocity gets very low
        if (Math.abs(this.state.currentVelocity) < this.config.minDriftSpeed) { 
            this.smoothScrollTo(targetY, 260);
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
        // Ease-in-out cubic for a gentle start and smooth stop
        const easeInOutCubic = (t) => t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
        
        const animate = () => {
            const elapsed = performance.now() - startTime;
            const progress = Math.min(1, elapsed / duration);
            const easedProgress = easeInOutCubic(progress);
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
                // Keep some space for the fixed nav; bias a bit lower to ensure top UI remains reachable
                const offset = (viewportHeight / 2) - Math.min(navHeight, 80) * 0.5;
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
                    const visibilityBonus = visibility > 0.1 ? (1 - visibility * 0.5) : 1.25; // Reduce penalty to ease into early posts
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