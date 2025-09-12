import { fetchWithCSRF } from './services/api.js';
import { getImageURL, sanitizeAndRenderMarkdown, escapeHTML } from './utils.js';

export default class Gallery {
    constructor(app) {
        this.app = app;
        this.gallery = document.getElementById('gallery');
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
        this._forcedColumnIndex = null;

        // Image lazy-loading state
        this.imageObserver = null;
        this.lazyQueue = [];
        this.currentImageLoads = 0;
        this.maxConcurrentImageLoads = 4; // cap concurrent downloads to reduce server stress

        // Queue of items not yet rendered as cards
        this.unrendered = [];
    }

    reset() {
        // Reset lazy loading pipeline
        try { if (this.imageObserver) this.imageObserver.disconnect(); } catch {}
        this.lazyQueue = [];
        this.currentImageLoads = 0;
        this.unrendered = [];
        // Reset managed masonry layout
        this.disableManagedMasonry();
    }

    clear() {
        this.gallery.innerHTML = '';
        this.gallery.classList.remove('settings-mode');
    }

    async loadImages() {
        if (this.app.routeMode !== 'home' && this.app.routeMode !== 'profile') return;
        if (this.app.loading || !this.app.hasMore) return;
        this.app.loading = true;
        this.showLoader();
        try {
            let resp = null;
            if (this.app.routeMode === 'home') {
                resp = await fetchWithCSRF(`/api/feed?page=${this.app.page}`, { credentials: 'include' });
            } else {
                const uname = this.app.profileUsername || decodeURIComponent(location.pathname.slice(2));
                const tab = this.app.profileTab || 'posts';
                const url = (tab === 'collections')
                    ? `/api/users/${encodeURIComponent(uname)}/collections?page=${this.app.page}`
                    : `/api/users/${encodeURIComponent(uname)}/images?page=${this.app.page}`;
                resp = await fetchWithCSRF(url, { credentials: 'include' });
            }
            if (resp.ok) {
                const data = await resp.json();
                if (data.images && data.images.length > 0) {
					this.enqueueUnrendered(data.images);
					this.maybeRevealCards();
                    this.app.page++;
                } else {
                    this.app.hasMore = false;
                }
            } else {
                this.app.hasMore = false;
            }
        } catch (e) {
            this.app.hasMore = false;
        } finally {
            this.app.loading = false;
            this.hideLoader();
        }
    }

    renderDemoImages() {
        const epoch = this.app.renderEpoch;
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
        if (this.app.isRestoring) {
            demoImages.forEach((image) => this.createImageCard(image));
        } else {
            demoImages.forEach((image, index) => {
                const tid = this.app.trackTimeout(setTimeout(() => { this.app.untrackTimeout(tid); if (epoch !== this.app.renderEpoch) return; this.createImageCard(image); }, index * 80));
            });
        }
    }

    renderImages(images) {
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

        const epoch = this.app.renderEpoch;
        let revealed = 0;
        const useManaged = (this.masonry && this.masonry.enabled && this.masonry.columnCount > 1 && window.innerWidth > 900);
        for (let i = 0; i < toReveal; i++) {
            const item = this.unrendered.shift();
            if (!item) break;
            if (this.app.isRestoring) {
                if (useManaged) this._forcedColumnIndex = (this.masonry.appendedCount % this.masonry.columnCount);
                this.createImageCard(item);
            } else {
                const tid = this.app.trackTimeout(setTimeout(() => {
                    this.app.untrackTimeout(tid);
                    if (epoch !== this.app.renderEpoch) return;
                    if (useManaged) this._forcedColumnIndex = ((this.masonry.appendedCount) % this.masonry.columnCount);
                    this.createImageCard(item);
                }, Math.min(40, i * 20)));
            }
            revealed++;
        }
        return revealed;
    }

    async topUpBelowViewport(maxCycles = 3) {
        const viewportH = window.innerHeight || document.documentElement.clientHeight;
        let cycles = 0;
        while (cycles < Math.max(1, maxCycles)) {
            const doc = document.documentElement;
            const bottomSpace = doc.scrollHeight - (window.scrollY + viewportH);
            if (bottomSpace >= Math.floor(viewportH * 0.75)) break;
            let revealed = 0;
            if (this.unrendered && this.unrendered.length > 0) {
                revealed = this.maybeRevealCards((window.innerWidth <= 600) ? 4 : 8);
            } else if (this.app.hasMore && !this.app.loading && (this.app.routeMode === 'home' || this.app.routeMode === 'profile')) {
                await this.loadImages();
                revealed = this.maybeRevealCards((window.innerWidth <= 600) ? 4 : 8);
            } else {
                break;
            }
            cycles++;
            if (!revealed) break;
            await new Promise(r => setTimeout(r, 16));
        }
    }

    createImageCard(image) {
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
        const isOwner = !!this.app.currentUser && (this.app.currentUser.username === image.username);
        const isAdmin = !!this.app.currentUser && !!this.app.currentUser.is_admin;
        const isModerator = !!this.app.currentUser && !!this.app.currentUser.is_moderator;
        const canEdit = (onProfile && isOwner) || isAdmin || isModerator;

        let img = null;

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
            const imgURL = getImageURL(image.filename);
            img.dataset.src = imgURL;
            img.alt = image.original_name || image.title || '';
            img.loading = 'lazy';
            img.style.opacity = '0.001';
            if (Number.isFinite(image.width) && Number.isFinite(image.height) && image.width > 0 && image.height > 0) {
                try { img.width = Math.floor(image.width); img.height = Math.floor(image.height); } catch {}
                try { img.style.aspectRatio = `${image.width} / ${image.height}`; } catch {}
            }
            img.style.background = 'var(--surface)';
            const nsfwPref = (this.app.currentUser?.nsfw_pref || (this.app.currentUser?.show_nsfw ? 'show' : 'hide'));
            const shouldBlur = image.is_nsfw && nsfwPref === 'blur';
            const shouldHide = image.is_nsfw && (!this.app.currentUser || nsfwPref === 'hide');
            if (shouldHide) {
                card.innerHTML = `
                    <div class="image-meta">
                        <div class="image-title">Content Hidden</div>
                        <div class="image-author">NSFW content filtered</div>
                    </div>`;
                this.appendCardToMasonry(card);
                return;
            }
            if (shouldBlur) {
                card.classList.add('nsfw-blurred');
                card.appendChild(img);
                card._nsfwRevealed = false;
            } else {
                card.appendChild(img);
            }

            const meta = document.createElement('div');
            meta.className = 'image-meta';
            const username = image.username || image.author || 'Unknown';
            const captionHtml = image.caption ? `<div class="image-caption" style="margin-top:4px;color:var(--text-secondary);font-size:0.8rem">${sanitizeAndRenderMarkdown(String(image.caption))}</div>` : '';
            const actions = canEdit ? `
                <div class="image-actions" style="display:flex;gap:2px;align-items:center;flex-shrink:0">
                  <button title="Edit" class="like-btn" data-act="edit" data-id="${image.id}" style="width:28px;height:28px;padding:0;color:var(--text-secondary)">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 1 1 3 3L7 19l-4 1 1-4Z"/></svg>
                  </button>
                  <button title="Delete" class="like-btn" data-act="delete" data-id="${image.id}" style="width:28px;height:28px;padding:0;color:#ff6666">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </div>` : '';
            const collectBtn = (!isOwner) ? `<button title="Collect" class="like-btn collect-btn${(this.app._myCollectedSet && this.app._myCollectedSet.has(String(image.id))) ? ' collected' : ''}" data-act="collect" data-id="${image.id}" style="width:32px;height:32px;padding:0;font-size:16px;opacity:0.85">${(this.app._myCollectedSet && this.app._myCollectedSet.has(String(image.id))) ? '✦' : '✧'}</button>` : '';
            meta.innerHTML = `
                <div style="display:flex;align-items:center;justify-content:space-between;gap:12px">
                  <div style="min-width:0">
                    <div class="image-title" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis"><a href="/i/${encodeURIComponent(image.id)}" class="image-link" style="color:inherit;text-decoration:none">${escapeHTML(String((image.title || image.original_name || 'Untitled')).trim())}</a></div>
                    <div class="image-author" style="font-family:var(--font-mono)"><a href="/@${encodeURIComponent(username)}" style="color:inherit;text-decoration:none">@${escapeHTML(String(username))}</a></div>
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
                            this.app.persistListState();
                        }
                    } catch {}
                    this.app.router.navigateTo(a.getAttribute('href'));
                    return;
                }
                const btn = e.target.closest('button');
                if (!btn) return;
                const act = btn.dataset.act;
                const id = btn.dataset.id;
                e.stopPropagation();
                if (act === 'collect') {
                    if (!this.app.currentUser) { this.app.showAuthModal(); return; }
                    btn.classList.toggle('collected');
                    btn.textContent = btn.classList.contains('collected') ? '✦' : '✧';
                    try {
                        const resp = await fetchWithCSRF(`/api/images/${id}/collect`, { method:'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include' });
                        if (!resp.ok) {
                            btn.classList.toggle('collected');
                            btn.textContent = btn.classList.contains('collected') ? '✦' : '✧';
                            if (resp.status === 401) { this.app.currentUser = null; this.app.auth.checkAuth(); this.app.auth.showAuthModal(); }
                            else { this.app.showNotification('Collect failed', 'error'); }
                        }
                        if (!this.app._myCollectedSet) this.app._myCollectedSet = new Set();
                        if (btn.classList.contains('collected')) this.app._myCollectedSet.add(String(id)); else this.app._myCollectedSet.delete(String(id));
                    } catch { btn.classList.toggle('collected'); btn.textContent = btn.classList.contains('collected') ? '✦' : '✧'; }
                } else if (act === 'delete') {
                    const ok = await this.app.showConfirm('Delete image?');
                    if (ok) {
                        const resp = await fetchWithCSRF(`/api/images/${id}`, { method: 'DELETE', credentials: 'include' });
                        if (resp.status === 204) { card.remove(); this.app.showNotification('Image deleted'); } else { this.app.showNotification('Delete failed', 'error'); }
                    }
                } else if (act === 'edit') {
                    this.app.openEditModal(image, card);
                }
            });
            card.appendChild(meta);

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
                img.addEventListener('animationend', function onEnd() {
                    card.classList.remove('leaving');
                    img.removeEventListener('animationend', onEnd);
                });
            });

            if (image.caption) {
                let captionExpanded = false;
                const toggleCaption = (ev) => {
                    ev.stopPropagation();
                    const cap = meta.querySelector('.image-caption');
                    if (!cap) return;
                    cap.classList.toggle('expanded');
                    captionExpanded = cap.classList.contains('expanded');
                };
                meta.addEventListener('click', (ev) => {
                    const capEl = ev.target.closest('.image-caption');
                    if (!capEl) return;
                    const link = ev.target.closest('a');
                    if (link) { ev.stopPropagation(); return; }
                    toggleCaption(ev);
                });
            }
        }

        let clickTimer = null;
        card.addEventListener('click', (e) => {
            if (e.detail === 2) return;
            if (card.classList.contains('nsfw-blurred') && !card._nsfwRevealed) {
                card._nsfwRevealed = true;
                card.classList.add('revealing');
                setTimeout(() => {
                    card.classList.remove('nsfw-blurred', 'revealing');
                    card.classList.add('nsfw-revealed');
                }, 1800);
                e.stopPropagation();
                e.preventDefault();
                return;
            }

            clearTimeout(clickTimer);
            clickTimer = setTimeout(() => {
                this.app.openLightbox(image);
            }, 180);
        });
        card.addEventListener('dblclick', (e) => {
            clearTimeout(clickTimer);
            e.preventDefault();
            e.stopPropagation();
            if (!this.app.currentUser) { this.app.auth.showAuthModal(); return; }
            if (this.app.currentUser && this.app.currentUser.username === image.username) return;
            const metaBtn = card.querySelector('button.collect-btn');
            if (metaBtn) this.app.toggleCollect(image.id, metaBtn);
            else this.app.toggleCollect(image.id);
        });
        this.appendCardToMasonry(card);

        if (img && img.dataset && img.dataset.src) {
            this.registerImageForLazyLoad(img);
        }
    }

    // Managed masonry: explicit column containers for efficient initial load
    enableManagedMasonry() {
        // Temporarily disable managed masonry on profile pages to fix a desktop rendering issue
        if (this.app.routeMode === 'profile') {
            this.disableManagedMasonry();
            return;
        }
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

    computeLazyBuffer() {
		if (this.masonry && this.masonry.enabled) {
            const rowH = this.masonry.rowHeightHint || 400;
            // Two rows plus some breathing room
            return Math.min(1600, Math.max(600, Math.round(rowH * 2.25)));
        }
		// On mobile, use a smaller buffer to avoid over-eager loading
		return (window.innerWidth <= 600) ? 250 : 600;
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
}
