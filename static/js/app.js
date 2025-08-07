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
        await this.loadImages();
        this.setupInfiniteScroll();
        this.setupLightbox();
        this.setupUpload();
        this.animateOnScroll();
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
        // Demo images for testing the UI
        const demoImages = [
            { id: '1', filename: 'demo1.jpg', original_name: 'AI Cityscape', username: 'aiartist', blurhash: 'L6P?B=WB2Yk=}8^B55RqDNx]1k4n', dominant_color: '#1a1a2e' },
            { id: '2', filename: 'demo2.jpg', original_name: 'Digital Dreams', username: 'synthcreator', blurhash: 'L9QmpFRP4T9F0KD%RPTkD$Ip~qhj', dominant_color: '#16213e' },
            { id: '3', filename: 'demo3.jpg', original_name: 'Neural Portrait', username: 'pixelpoet', blurhash: 'LAB3EI9a0f4oD%kCx]xur?E1IVRj', dominant_color: '#0f3460' },
        ];
        this.renderImages(demoImages);
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