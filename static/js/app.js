// static/js/app.js
class Trough {
    constructor() {
        this.images = [];
        this.page = 1;
        this.loading = false;
        this.hasMore = true;
        this.gallery = document.getElementById('gallery');
        this.lightbox = document.getElementById('lightbox');
        this.uploadZone = document.getElementById('upload-zone');
    }

    async init() {
        this.setupAuth();
        await this.loadImages();
        this.setupInfiniteScroll();
        this.setupLightbox();
        this.setupUpload();
        this.animateOnScroll();
    }

    setupAuth() {
        const authBtn = document.getElementById('auth-btn');
        const token = localStorage.getItem('token');
        
        if (token) {
            // User is logged in
            authBtn.textContent = 'profile';
            authBtn.onclick = () => this.showProfile();
        } else {
            // User is not logged in
            authBtn.textContent = 'enter';
            authBtn.onclick = () => this.showAuthModal();
        }
    }

    showAuthModal() {
        this.createAuthModal();
    }

    createAuthModal() {
        // Remove existing modal
        const existingModal = document.getElementById('auth-modal');
        if (existingModal) {
            existingModal.remove();
        }

        const modal = document.createElement('div');
        modal.id = 'auth-modal';
        modal.style.cssText = `
            position: fixed;
            inset: 0;
            z-index: 2000;
            background: rgba(0,0,0,0.95);
            backdrop-filter: blur(20px);
            display: flex;
            align-items: center;
            justify-content: center;
            animation: fadeIn 0.3s ease;
        `;

        modal.innerHTML = `
            <div style="
                background: #1a1a1a;
                border-radius: 1rem;
                padding: 3rem;
                width: 90%;
                max-width: 400px;
                animation: slideUp 0.3s ease;
            ">
                <h2 style="
                    font-size: 1.5rem;
                    font-weight: 300;
                    margin-bottom: 2rem;
                    text-align: center;
                    color: white;
                ">Welcome to trough</h2>
                
                <div id="auth-tabs" style="
                    display: flex;
                    margin-bottom: 2rem;
                    border-bottom: 1px solid #333;
                ">
                    <button id="login-tab" class="auth-tab active" data-tab="login">Login</button>
                    <button id="register-tab" class="auth-tab" data-tab="register">Register</button>
                </div>

                <form id="auth-form">
                    <div id="login-form">
                        <input type="email" id="login-email" placeholder="Email" required style="
                            width: 100%;
                            padding: 1rem;
                            margin-bottom: 1rem;
                            background: #2a2a2a;
                            border: 1px solid #333;
                            border-radius: 0.5rem;
                            color: white;
                            font-size: 1rem;
                        ">
                        <input type="password" id="login-password" placeholder="Password" required style="
                            width: 100%;
                            padding: 1rem;
                            margin-bottom: 1.5rem;
                            background: #2a2a2a;
                            border: 1px solid #333;
                            border-radius: 0.5rem;
                            color: white;
                            font-size: 1rem;
                        ">
                    </div>
                    
                    <div id="register-form" style="display: none;">
                        <input type="text" id="register-username" placeholder="Username" style="
                            width: 100%;
                            padding: 1rem;
                            margin-bottom: 1rem;
                            background: #2a2a2a;
                            border: 1px solid #333;
                            border-radius: 0.5rem;
                            color: white;
                            font-size: 1rem;
                        ">
                        <input type="email" id="register-email" placeholder="Email" style="
                            width: 100%;
                            padding: 1rem;
                            margin-bottom: 1rem;
                            background: #2a2a2a;
                            border: 1px solid #333;
                            border-radius: 0.5rem;
                            color: white;
                            font-size: 1rem;
                        ">
                        <input type="password" id="register-password" placeholder="Password" style="
                            width: 100%;
                            padding: 1rem;
                            margin-bottom: 1.5rem;
                            background: #2a2a2a;
                            border: 1px solid #333;
                            border-radius: 0.5rem;
                            color: white;
                            font-size: 1rem;
                        ">
                    </div>

                    <button type="submit" id="auth-submit" style="
                        width: 100%;
                        padding: 1rem;
                        background: var(--accent);
                        color: black;
                        border: none;
                        border-radius: 0.5rem;
                        font-size: 1rem;
                        font-weight: 500;
                        cursor: pointer;
                        transition: all 0.2s;
                        margin-bottom: 1rem;
                    ">Login</button>

                    <div id="auth-error" style="
                        color: #ff4444;
                        font-size: 0.875rem;
                        text-align: center;
                        display: none;
                    "></div>
                </form>

                <button onclick="document.getElementById('auth-modal').remove()" style="
                    position: absolute;
                    top: 1rem;
                    right: 1rem;
                    background: none;
                    border: none;
                    color: #666;
                    font-size: 1.5rem;
                    cursor: pointer;
                ">×</button>
            </div>
        `;

        document.body.appendChild(modal);
        this.setupAuthModalEvents();
        
        // Close on background click
        modal.addEventListener('click', (e) => {
            if (e.target === modal) {
                modal.remove();
            }
        });
    }

    setupAuthModalEvents() {
        const tabs = document.querySelectorAll('.auth-tab');
        const loginForm = document.getElementById('login-form');
        const registerForm = document.getElementById('register-form');
        const submitBtn = document.getElementById('auth-submit');
        const form = document.getElementById('auth-form');

        // Tab switching
        tabs.forEach(tab => {
            tab.addEventListener('click', () => {
                tabs.forEach(t => t.classList.remove('active'));
                tab.classList.add('active');
                
                const tabType = tab.dataset.tab;
                if (tabType === 'login') {
                    loginForm.style.display = 'block';
                    registerForm.style.display = 'none';
                    submitBtn.textContent = 'Login';
                } else {
                    loginForm.style.display = 'none';
                    registerForm.style.display = 'block';
                    submitBtn.textContent = 'Register';
                }
            });
        });

        // Form submission
        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            const isLogin = document.getElementById('login-tab').classList.contains('active');
            
            if (isLogin) {
                await this.handleLogin();
            } else {
                await this.handleRegister();
            }
        });
    }

    async handleLogin() {
        const email = document.getElementById('login-email').value;
        const password = document.getElementById('login-password').value;
        const errorDiv = document.getElementById('auth-error');

        try {
            const response = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, password })
            });

            const data = await response.json();

            if (response.ok) {
                localStorage.setItem('token', data.token);
                localStorage.setItem('user', JSON.stringify(data.user));
                document.getElementById('auth-modal').remove();
                this.setupAuth(); // Update auth button
                this.showSuccessMessage('Logged in successfully!');
            } else {
                errorDiv.textContent = data.error || 'Login failed';
                errorDiv.style.display = 'block';
            }
        } catch (error) {
            errorDiv.textContent = 'Network error. Please try again.';
            errorDiv.style.display = 'block';
        }
    }

    async handleRegister() {
        const username = document.getElementById('register-username').value;
        const email = document.getElementById('register-email').value;
        const password = document.getElementById('register-password').value;
        const errorDiv = document.getElementById('auth-error');

        try {
            const response = await fetch('/api/register', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, email, password })
            });

            const data = await response.json();

            if (response.ok) {
                localStorage.setItem('token', data.token);
                localStorage.setItem('user', JSON.stringify(data.user));
                document.getElementById('auth-modal').remove();
                this.setupAuth(); // Update auth button
                this.showSuccessMessage(`Welcome to trough, ${data.user.username}!`);
            } else {
                errorDiv.textContent = data.error || 'Registration failed';
                errorDiv.style.display = 'block';
            }
        } catch (error) {
            errorDiv.textContent = 'Network error. Please try again.';
            errorDiv.style.display = 'block';
        }
    }

    showProfile() {
        const user = JSON.parse(localStorage.getItem('user'));
        alert(`Profile: ${user.username}\n\nFeature coming soon!`);
    }

    showSuccessMessage(message) {
        const toast = document.createElement('div');
        toast.style.cssText = `
            position: fixed;
            top: 2rem;
            right: 2rem;
            background: var(--accent);
            color: black;
            padding: 1rem 2rem;
            border-radius: 0.5rem;
            font-weight: 500;
            z-index: 3000;
            animation: slideInRight 0.3s ease;
        `;
        toast.textContent = message;
        document.body.appendChild(toast);
        
        setTimeout(() => toast.remove(), 3000);
    }

    async toggleLike(imageId) {
        const token = localStorage.getItem('token');
        
        if (!token) {
            this.showAuthModal();
            return;
        }

        const likeBtn = document.querySelector(`[onclick="window.trough.toggleLike('${imageId}')"]`);
        if (!likeBtn) return;

        // Optimistic UI update
        const isLiked = likeBtn.classList.contains('liked');
        likeBtn.classList.toggle('liked');
        
        if (!isLiked) {
            likeBtn.style.color = '#ff4757';
            likeBtn.style.transform = 'scale(1.2)';
            setTimeout(() => {
                likeBtn.style.transform = 'scale(1)';
            }, 200);
        } else {
            likeBtn.style.color = '';
        }

        try {
            const response = await fetch(`/api/images/${imageId}/like`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${token}`,
                    'Content-Type': 'application/json'
                }
            });

            const data = await response.json();
            
            if (!response.ok) {
                // Revert optimistic update on error
                likeBtn.classList.toggle('liked');
                likeBtn.style.color = '';
                this.showErrorMessage(data.error || 'Failed to like image');
            }
        } catch (error) {
            // Revert optimistic update on error
            likeBtn.classList.toggle('liked');
            likeBtn.style.color = '';
            this.showErrorMessage('Network error. Please try again.');
        }
    }

    showErrorMessage(message) {
        const toast = document.createElement('div');
        toast.style.cssText = `
            position: fixed;
            top: 2rem;
            right: 2rem;
            background: #ff4444;
            color: white;
            padding: 1rem 2rem;
            border-radius: 0.5rem;
            font-weight: 500;
            z-index: 3000;
            animation: slideInRight 0.3s ease;
        `;
        toast.textContent = message;
        document.body.appendChild(toast);
        
        setTimeout(() => toast.remove(), 3000);
    }

    async loadImages() {
        if (this.loading || !this.hasMore) return;
        
        this.loading = true;
        this.showLoader();
        
        try {
            const res = await fetch(`/api/feed?page=${this.page}`);
            const data = await res.json();
            
            if (data.images.length === 0) {
                this.hasMore = false;
                return;
            }
            
            this.renderImages(data.images);
            this.page++;
        } catch (error) {
            console.error('Failed to load images:', error);
            // Show placeholder images for demo
            this.renderDemoImages();
        } finally {
            this.loading = false;
            this.hideLoader();
        }
    }

    renderDemoImages() {
        // Create beautiful gradient placeholders instead of external images
        this.createPlaceholderImages();
    }

    createPlaceholderImages() {
        const placeholders = [
            { id: '1', color: '#1a1a2e', name: 'Neural Landscape', user: 'aiartist' },
            { id: '2', color: '#16213e', name: 'Digital Dreams', user: 'synthcreator' },
            { id: '3', color: '#0f3460', name: 'Quantum Vision', user: 'pixelpoet' },
            { id: '4', color: '#2d1b69', name: 'Cyber Garden', user: 'techno_artist' },
            { id: '5', color: '#1e3a8a', name: 'Neon Waves', user: 'futurescape' },
            { id: '6', color: '#7c2d12', name: 'AI Portrait', user: 'digital_soul' },
        ];

        placeholders.forEach((placeholder, index) => {
            const card = document.createElement('div');
            card.className = 'image-card';
            card.style.animationDelay = `${index * 0.1}s`;
            
            const height = 200 + Math.random() * 300;
            card.style.minHeight = `${height}px`;
            card.style.background = `linear-gradient(135deg, ${placeholder.color}, ${this.lightenColor(placeholder.color, 20)})`;
            card.style.borderRadius = '0.5rem';
            card.style.position = 'relative';
            card.style.cursor = 'zoom-in';
            
            const overlay = document.createElement('div');
            overlay.style.position = 'absolute';
            overlay.style.bottom = '0';
            overlay.style.left = '0';
            overlay.style.right = '0';
            overlay.style.background = 'linear-gradient(transparent, rgba(0,0,0,0.7))';
            overlay.style.padding = '2rem 1rem 1rem';
            overlay.style.color = 'white';
            overlay.style.fontSize = '0.875rem';
            overlay.innerHTML = `
                <div style="font-weight: 500; margin-bottom: 0.25rem;">${placeholder.name}</div>
                <div style="opacity: 0.8;">@${placeholder.user}</div>
            `;
            
            card.appendChild(overlay);
            card.addEventListener('click', () => this.openPlaceholderLightbox(placeholder));
            
            this.gallery.appendChild(card);
        });
    }

    lightenColor(color, percent) {
        const num = parseInt(color.replace("#",""), 16);
        const amt = Math.round(2.55 * percent);
        const R = (num >> 16) + amt;
        const G = (num >> 8 & 0x00FF) + amt;
        const B = (num & 0x0000FF) + amt;
        return "#" + (0x1000000 + (R < 255 ? R < 1 ? 0 : R : 255) * 0x10000 +
            (G < 255 ? G < 1 ? 0 : G : 255) * 0x100 +
            (B < 255 ? B < 1 ? 0 : B : 255)).toString(16).slice(1);
    }

    renderImages(images) {
        images.forEach((image, index) => {
            const card = document.createElement('div');
            card.className = 'image-card';
            card.style.animationDelay = `${index * 0.05}s`;
            
            // Use blurhash/dominant color for beautiful loading
            if (image.dominant_color) {
                card.style.backgroundColor = image.dominant_color;
            }
            
            // Create skeleton while loading
            const skeleton = document.createElement('div');
            skeleton.className = 'image-skeleton';
            card.appendChild(skeleton);
            
            const img = new Image();
            img.onload = () => {
                card.removeChild(skeleton);
                card.appendChild(img);
                // Trigger reflow for smooth animation
                card.offsetHeight;
            };
            
            img.onerror = () => {
                // Fallback to gradient background on error
                card.removeChild(skeleton);
                card.style.minHeight = '200px';
                card.style.background = `linear-gradient(135deg, ${image.dominant_color || '#1a1a2e'}, #000)`;
                const placeholder = document.createElement('div');
                placeholder.innerHTML = `<p style="color: rgba(255,255,255,0.5); text-align: center; padding: 2rem; font-size: 0.875rem;">${image.original_name}</p>`;
                card.appendChild(placeholder);
            };
            
            // Use placeholder for demo (since we don't have actual images)
            img.src = `https://picsum.photos/400/600?random=${image.id}`;
            img.alt = image.original_name;
            img.style.width = '100%';
            img.style.height = 'auto';
            img.style.display = 'block';
            
            card.addEventListener('click', () => this.openLightbox(image));
            
            this.gallery.appendChild(card);
        });
    }

    openLightbox(image) {
        const img = document.getElementById('lightbox-img');
        img.src = `https://picsum.photos/800/1200?random=${image.id}`;
        
        const user = document.getElementById('lightbox-user');
        user.textContent = `@${image.username}`;
        user.href = `/@${image.username}`;
        
        this.currentImage = image;
        this.lightbox.classList.add('active');
        document.body.style.overflow = 'hidden';
        
        // ESC to close
        const closeHandler = (e) => {
            if (e.key === 'Escape') {
                this.closeLightbox();
                document.removeEventListener('keydown', closeHandler);
            }
        };
        document.addEventListener('keydown', closeHandler);
        
        // Click outside to close
        this.lightbox.onclick = (e) => {
            if (e.target === this.lightbox) {
                this.closeLightbox();
            }
        };
    }

    openPlaceholderLightbox(placeholder) {
        const lightboxContent = this.lightbox.querySelector('.lightbox-content');
        
        // Clear existing content
        lightboxContent.innerHTML = '';
        
        // Create placeholder lightbox content
        const placeholderDiv = document.createElement('div');
        placeholderDiv.style.width = '600px';
        placeholderDiv.style.height = '800px';
        placeholderDiv.style.background = `linear-gradient(135deg, ${placeholder.color}, ${this.lightenColor(placeholder.color, 20)})`;
        placeholderDiv.style.borderRadius = '0.5rem';
        placeholderDiv.style.position = 'relative';
        placeholderDiv.style.display = 'flex';
        placeholderDiv.style.alignItems = 'center';
        placeholderDiv.style.justifyContent = 'center';
        placeholderDiv.style.color = 'white';
        placeholderDiv.style.fontSize = '2rem';
        placeholderDiv.style.fontWeight = '300';
        placeholderDiv.innerHTML = placeholder.name;
        
        // Create info overlay
        const infoDiv = document.createElement('div');
        infoDiv.className = 'lightbox-info';
        infoDiv.innerHTML = `
            <a class="lightbox-user" href="/@${placeholder.user}">@${placeholder.user}</a>
            <button class="lightbox-like" onclick="window.trough.toggleLike('${placeholder.id}')">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                </svg>
            </button>
        `;
        
        lightboxContent.appendChild(placeholderDiv);
        lightboxContent.appendChild(infoDiv);
        
        this.currentImage = { id: placeholder.id, username: placeholder.user };
        this.lightbox.classList.add('active');
        document.body.style.overflow = 'hidden';
        
        // ESC to close
        const closeHandler = (e) => {
            if (e.key === 'Escape') {
                this.closeLightbox();
                document.removeEventListener('keydown', closeHandler);
            }
        };
        document.addEventListener('keydown', closeHandler);
        
        // Click outside to close
        this.lightbox.onclick = (e) => {
            if (e.target === this.lightbox) {
                this.closeLightbox();
            }
        };
    }

    closeLightbox() {
        this.lightbox.classList.remove('active');
        document.body.style.overflow = '';
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

    setupUpload() {
        let dragCounter = 0;
        
        document.addEventListener('dragenter', (e) => {
            e.preventDefault();
            dragCounter++;
            if (dragCounter === 1) {
                this.uploadZone.classList.add('active');
            }
        });
        
        document.addEventListener('dragleave', (e) => {
            e.preventDefault();
            dragCounter--;
            if (dragCounter === 0) {
                this.uploadZone.classList.remove('active');
            }
        });
        
        document.addEventListener('dragover', (e) => {
            e.preventDefault();
        });
        
        document.addEventListener('drop', async (e) => {
            e.preventDefault();
            dragCounter = 0;
            this.uploadZone.classList.remove('active');
            
            const files = Array.from(e.dataTransfer.files);
            for (const file of files) {
                if (file.type.startsWith('image/')) {
                    await this.uploadImage(file);
                }
            }
        });
    }

    async uploadImage(file) {
        const formData = new FormData();
        formData.append('image', file);
        
        // Show upload progress with style
        const progressBar = this.createProgressBar();
        document.body.appendChild(progressBar);
        
        try {
            const res = await fetch('/api/upload', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${localStorage.getItem('token')}`
                },
                body: formData
            });
            
            if (res.ok) {
                const image = await res.json();
                // Prepend to gallery with animation
                this.renderImages([image]);
                progressBar.classList.add('complete');
            } else {
                progressBar.classList.add('error');
            }
        } catch (error) {
            console.error('Upload failed:', error);
            progressBar.classList.add('error');
        } finally {
            setTimeout(() => progressBar.remove(), 1000);
        }
    }

    createProgressBar() {
        const bar = document.createElement('div');
        bar.className = 'upload-progress';
        bar.innerHTML = '<div class="upload-progress-bar"></div>';
        return bar;
    }

    showLoader() {
        if (!document.querySelector('.loader')) {
            const loader = document.createElement('div');
            loader.className = 'loader';
            document.body.appendChild(loader);
        }
    }

    hideLoader() {
        const loader = document.querySelector('.loader');
        if (loader) loader.remove();
    }

    animateOnScroll() {
        const observer = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    entry.target.classList.add('visible');
                }
            });
        }, { threshold: 0.1 });
        
        // Observe existing cards
        document.querySelectorAll('.image-card').forEach(card => {
            observer.observe(card);
        });
        
        // Store observer for future use
        this.intersectionObserver = observer;
    }

    // Method to observe new cards as they're added
    observeNewCard(card) {
        if (this.intersectionObserver) {
            this.intersectionObserver.observe(card);
        }
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    const app = new Trough();
    app.init();
    
    // Make app globally accessible for debugging
    window.trough = app;
});