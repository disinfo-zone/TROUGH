// MagneticScroll: gentle, oozing drift-to-focus behavior
export default class MagneticScroll {
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
