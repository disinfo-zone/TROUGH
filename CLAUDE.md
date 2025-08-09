# Agent Instructions: Building trough

## Vision
Build "trough" — an impossibly slick, minimalist web app for AI‑generated images. Every pixel matters. Every interaction should feel butter‑smooth. This is not just an image gallery; it’s an object of design. The interface gets out of the way and frames the art with ruthless restraint.

Trough only allows AI images to be uploaded (verified via EXIF data; we preserve and display key fields). The front page is a continuous river of uploads. Users have profile pages at https://url.com/@profile. When a user is logged in, the top of their page exposes an upload field followed by their feed. NSFW images are hidden to non‑logged‑in users (uploaders can flag; anyone can report).

Everything is minimal with a brutal and exquisite focus on the art — but never at the expense of usability.

## Product Overview
- **What it is**: A zero‑friction, high‑taste image river for AI artwork. Upload, browse, savor.
- **Who it’s for**: Artists, model‑tinkerers, curators, and anyone who enjoys visually immersive streams.
- **Why it exists**: To showcase AI visuals in a space that elevates the work and disappears as UI.
- **Core constraints**:
  - AI‑only uploads (EXIF signals verified). No manual photos.
  - Art‑first layout. Chrome is quiet; content is loud.
  - Performance is a feature. Motion is felt, not noticed.
- **Brand voice**: Effortlessly cool. Minimalist. Precise. Confident. Humane.

## Experience Architecture
- **Landing / River**: Infinite masonry-like stream, soft‑start skeletons, blurhash reveal, zero layout shift.
- **Lightbox**: Edge‑to‑edge image; subtle chrome overlays; likes; EXIF highlights; keyboard nav.
- **Profile (`/@handle`)**: Header with avatar/handle/bio; user stream; when logged-in owner: upload control pinned.
- **Upload**: Drag‑and‑drop + click; EXIF scan; NSFW flag; progress; helpful errors.
- **Auth modal**: Login/register in one panel; client validation; crisp error messaging.
- **Reporting / NSFW gating**: Clear affordance; hidden for guests; opt‑in preferences for users.
- **404 / Empty states**: Purposefully minimal, fast paths back to river.

## Current Status (Repo Snapshot)
- Backend (Go + Fiber): Implemented
  - Routes: `/api/register`, `/api/login`, `/api/me`, `/api/feed`, `/api/images/:id`, `/api/upload` (auth), `/api/images/:id/like` (auth), `/api/users/:username`, `/api/users/:username/images`
  - JWT auth with password hashing (bcrypt)
  - Image upload with processing (blurhash, dominant color), EXIF scan for AI signatures
  - NSFW gating via user preference
  - Auto-migrations on startup
  - Account (auth): `/api/me/profile` (GET/PATCH), `/api/me/account` (GET), `/api/me/email` (PATCH), `/api/me/password` (PATCH), `/api/me` (DELETE), `/api/me/avatar` (POST)
- Database (PostgreSQL): Implemented
  - Tables: `users`, `images`, `likes` with indexes
- Frontend (Vanilla JS/CSS/HTML): Implemented
  - Masonry-like gallery, lightbox, drag & drop upload
  - Auth modal (login/register) wired to API; token/user persisted to `localStorage`
  - Session validation via `/api/me`; sign out; simple profile menu; disabled submit + show/hide password controls
  - Loader/toast UI, infinite scroll
- Ops:
  - Docker (multi-stage) + docker-compose with DB healthcheck; `Makefile` provided
  - Config via `config.yaml` and `.env.example`
- Tests: Unit + integration tests present (handlers, services)

Note: Code examples here are intentionally minimal; see repo files for implementation details.

## Theme v2: Vital Minimalism
A complete theme rewrite that is new, vital, and unmistakable — yet ruthlessly usable. Think: surgical typography, sharp geometry, generous negative space, and subtle kinetic detail. Effortlessly cool without theatrics.

### Goals
- **Typographic authority**: crisp hierarchy; no generic feel.
- **Better vertical rhythm**: consistent scales; breathing room.
- **Denser card information**: without visual noise.
- **Richer lightbox**: cinematic focus; unobtrusive metadata.
- **Refined motion**: natural, responsive, GPU‑friendly.
- **Accessibility**: contrast, focus, keyboard, reduced motion.
- **Performance**: no layout shifts; image-first paint; transform/opacity animations.

### Visual Tenets
- **Art first**: backgrounds recede to near‑black/near‑paper; true blacks allowed.
- **Brutal forms, humane details**: sharp edges with soft micro‑interactions.
- **One accent**: sparingly used for state, focus, progress.
- **Texture by restraint**: thin rules, hairlines, subtle grain optional (CSS only).

### Deliverables
- Design tokens in `:root` inside `static/css/style.css` (no preprocessor required)
- Updated layout/components: nav, cards, lightbox, auth modal, upload, toasts
- Motion + focus system; reduced motion handling
- Accessibility pass (contrast ≥ 4.5:1 body; ≥ 3:1 large text/icons)

### Design Tokens (authoritative)
Place at top of `static/css/style.css` under a `:root` block.

```css
:root {
  /* Color */
  --color-bg:        #0a0a0a;
  --color-bg-elev:   #111213;
  --color-surface:   #161718;
  --color-fg:        #e7e7e7;
  --color-fg-muted:  #a8a8a8;
  --color-fg-subtle: #7a7a7a;
  --color-accent:    #7af0ff;   /* cyan mint */
  --color-accent-2:  #b9ff7a;   /* alt lime */
  --color-danger:    #ff5c5c;
  --color-warn:      #f2c94c;
  --color-ok:        #59e38f;
  --color-hairline:  #2a2a2a;

  /* Elevation & borders */
  --radius-none: 0px;
  --radius-s:    6px;
  --radius-m:    10px;
  --radius-l:    14px;
  --border-hair: 1px;

  /* Shadow (subtle, low-luminance) */
  --shadow-1: 0 1px 0 rgba(0,0,0,0.6);
  --shadow-2: 0 8px 24px rgba(0,0,0,0.35);
  --shadow-3: 0 24px 48px rgba(0,0,0,0.45);

  /* Spacing scale (8px base, with fine steps) */
  --space-1: 2px;
  --space-2: 4px;
  --space-3: 8px;
  --space-4: 12px;
  --space-5: 16px;
  --space-6: 24px;
  --space-7: 32px;
  --space-8: 48px;
  --space-9: 64px;

  /* Typography */
  --font-sans: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, Inter, "Helvetica Neue", Arial, Noto Sans, "Apple Color Emoji", "Segoe UI Emoji";
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;

  --text-xxs: 10px; /* micro labels */
  --text-xs:  12px; /* metadata */
  --text-s:   14px; /* secondary */
  --text-m:   16px; /* body */
  --text-l:   20px; /* h5 */
  --text-xl:  24px; /* h4 */
  --text-2xl: 32px; /* h3 */
  --text-3xl: 40px; /* h2 */
  --text-4xl: 56px; /* hero */

  --weight-regular:  450;
  --weight-medium:   550;
  --weight-semibold: 650;

  --leading-tight: 1.1;
  --leading-snug:  1.25;
  --leading-normal:1.45;

  /* Motion */
  --motion-fast: 120ms;
  --motion-base: 220ms;
  --motion-slow: 360ms;
  --ease-emph:   cubic-bezier(0.2, 0.8, 0.2, 1);
  --ease-smooth: cubic-bezier(0.22, 0.61, 0.36, 1);

  /* Focus */
  --focus-ring: 0 0 0 2px var(--color-accent), 0 0 0 6px rgba(122,240,255,0.2);

  /* Layout */
  --container-max: 1280px;
  --gutter:        var(--space-6);
  --grid-gap:      var(--space-6);
}

@media (prefers-color-scheme: light) {
  :root {
    --color-bg:       #f7f7f7;
    --color-bg-elev:  #ffffff;
    --color-surface:  #ffffff;
    --color-fg:       #0a0a0a;
    --color-fg-muted: #4a4a4a;
    --color-fg-subtle:#6b6b6b;
    --color-hairline: #e8e8e8;
  }
}

@media (prefers-reduced-motion: reduce) {
  * { animation: none !important; transition: none !important; }
}
```

### Components (key specs)
- **Navigation**
  - Left: wordmark; Center: negative space; Right: auth/profile.
  - Sticky; 56–64px; backdrop blur is always on (transparent glass). No bottom hairline border.
  - Hover/focus are color‑only; no heavy shadows.

- **Gallery Cards**
  - Aspect‑ratio preserved; no reflow on load.
  - Use blurhash + dominant color background; fade image in over 180–220ms.
  - Card chrome: like button (tap‑target ≥ 40px), like count, subtle metadata on hover.
  - Hairline border on hover; accent focus ring on keyboard focus.

- **Lightbox**
  - Full‑bleed image; background uses sampled dominant color at 6–8% with subtle radial.
  - Controls (top/bottom overlays): like, share, exif, close, prev/next (← / → keys).
  - Image enters with scale(0.985)→1 and 12px upward translate; overlay fades in after 50ms.

- **Upload**
  - Dropzone with dashed hairline; accent glow on drag‑over; progress bar using accent.
  - After upload, an edit modal presents Title, Caption, NSFW toggles (not inline on profile).
  - Clear validations: size/type; EXIF AI check; error toasts mapped to fields.

- **Profile**
  - Header with avatar/handle/bio; user stream; when owner: upload control (dropzone only) pinned.
  - Bio appears inline under header, left‑aligned, sanitized Markdown (links/bold/italic), 500 char max, inline editor with live count for owner.
  - Avatar uploads crop ~5% inwards (center) on save to remove edge/border artifacts.

- **Settings**
  - Single centered column (stacked): Username → Avatar → Email → Password → Delete.
  - Username change validates uniqueness (case‑insensitive) and blocks reserved names (`admin`, `root`, `system`, `support`, `moderator`, `owner`, `undefined`, `null`, etc.).
  - Password change requires Current, New, Confirm; strength meter must indicate sufficiently strong.
  - Delete account: explicit confirm field requiring `DELETE`.

- **Auth Modal**
  - One panel with tabs (Login/Register); real‑time validation; submit disabled until valid.
  - Success clears form and closes; failure shows exact cause; rate‑limit hints generic.

- **Toasts**
  - Bottom center; stacked; 3–4 seconds; progress bar; swipe to dismiss on touch.

- **Skeletons**
  - Use blurhash tiles; avoid shimmer; maintain aspect ratio; subtle scale‑y micro‑jitter removed (no CLS).

### Motion & Interaction
- Only animate opacity/transform; avoid layout-affecting props.
- Default duration 220ms; ease `--ease-smooth`; emphasis actions use `--ease-emph`.
- Tap/press affordances scale to 0.98 with 60ms return.
- Focus is always visible (custom ring); do not remove outlines.
- Reduced motion: disable non‑essential motion; keep clarity.

### Accessibility
- Body text contrast ≥ 4.5:1; large text/icons ≥ 3:1; test both themes.
- Keyboard: tab order logical; skip‑to‑content link; trap focus in modals; ESC closes.
- Focus ring visible against both light/dark; minimum 2px.
- Hit areas ≥ 40px; touch targets separated by ≥ 8px.
- All images have `alt`; decorative elements `aria-hidden="true"`.

### Performance
- No layout shift: reserve sizes with `aspect-ratio` and fixed placeholders.
- Image loading: `loading="lazy"`, `decoding="async"`, prioritize first row.
- Use `content-visibility: auto` for off‑screen sections where appropriate.
- Avoid heavy filters; prefer precomputed blurhash and transforms.
- Keep page chrome light: nav uses GPU‑friendly blur (transparent background) with no extra rules/borders.

## Implementation Plan (simple, no scaffolding)
1) **Introduce tokens** at the top of `static/css/style.css`. Replace ad‑hoc colors/sizing with variables.
2) **Reset + Base**: normalize element defaults; set typography scale, background/foreground, links, focus ring.
3) **Layout**: header/nav, container/gutters, responsive grid with consistent gaps.
4) **Components**: cards, lightbox, upload, auth modal, toasts. Keep HTML mostly as‑is; refine classes.
5) **States**: hover/active/focus, disabled/loading, error/success.
6) **Motion**: enter/exit transitions; micro‑press; reduced motion rules.
7) **A11y/Perf pass**: contrast audit, keyboard flow, CLS/LCP checks.

### Concrete Deliverables
- `static/css/style.css` updated with tokens, base, components, and states
- Minimal HTML class updates where necessary to hook styles
- No new dependencies; vanilla JS/CSS only

## Authentication UX (end‑to‑end polish)
- Client‑side validation with inline errors; disable submit during request
- Show/hide password; Enter submits; clear on success; focus management
- Persisted auth state in nav; sign out; token expiry detection and reset
- Settings password change mirrors registration: Current/New/Confirm + strength meter; block weak or mismatched values.
- Username changes enforce reserved list + uniqueness; clear error messaging.

## Recent UX Updates
- Unified upload flow across file picker and drag-and-drop: images upload first, then open a centered preview edit modal showing the image with Title/Caption/NSFW controls. Save before feed refresh.
- Edit modal improved: tall images are constrained (`max-height: 60vh`), and metadata controls live in a sticky footer area so fields are always visible.
- Image card actions: Edit/Delete controls are inline with title/author, right-aligned, using high-contrast SVG icons. Titles truncate with ellipsis to ensure actions never hide.
- Loader: centered and stable; no drift.
- Settings page introduced (stacked single column) with Username/Avatar/Email/Password/Delete flows.
- Nav is always a transparent blurred glass; no bottom hairline; sits tighter above content.
- Fields and body copy use mono; headers/buttons remain sans for hierarchy.
- Profile upload row no longer shows inline Title/Caption/NSFW; these live in the post‑upload edit modal.

## EXIF
- Backend writes uploads as high-quality JPEG while preserving XMP and extracting full EXIF into `images.exif_data`.
- Lightbox EXIF viewer now opens immediately with a loading state, parses both object and JSON-string formats, and falls back to basic metadata if EXIF is missing.
- Pending: Validate that all `/api/images/:id` responses include the expected `exif_data` structure across all file types; expand displayed fields with human-friendly labels if desired.

## Known Gaps / Quick Fixes
- WebP decode: we accept `image/webp` uploads; decoder registered. Confirm with sample file end‑to‑end.
- Frontend polish:
  - Profile view and settings UI are minimal; implement real profile header/edit modal
  - Like count: display + optimistic updates in gallery/lightbox
  - Better skeletons/blurhash placeholders on initial load
- Security/Hardening:
  - Use strong `JWT_SECRET` in production
  - Consider rate limiting for auth endpoints
  - Validate image metadata and file type more defensively
  - Enforce reserved usernames and uniqueness on user rename (done)
- Ops:
  - Ensure `.env` is used locally; confirm CORS origins for prod

## Notable Implementation Details
- Dockerfile: Go 1.23‑alpine (multi‑stage)
- docker‑compose: DB healthcheck and `restart: unless-stopped`
- DB migrations: executed automatically on app start in `db/connection.go`
- Testing: `tests/handlers` and `tests/services` present; `Makefile` includes coverage target

## Critical Success Metrics

### Performance
- [ ] Images visually ready within 100–200ms after container paint (cached) / <1s cold
- [ ] Infinite scroll stays jitter‑free
- [ ] Zero measurable layout shift (CLS ≈ 0)
- [ ] Animations at 60fps (no long main‑thread tasks)

### Aesthetics
- [ ] Typography reads crisp at all sizes/densities
- [ ] Spacing grid is consistent and calm
- [ ] Motion feels intentional and minimal
- [ ] Dark theme is truly dark without crushing detail
- [ ] Loading states feel designed, not default

### User Experience
- [ ] Upload is drag‑and‑drop simple; errors are helpful
- [ ] Navigation is intuitive; keyboard workflows are first‑class
- [ ] Guests are safe from NSFW by default; reporting is obvious and low‑friction
- [ ] Mobile experience is flawless across common viewports

## Nice‑to‑Have (Future)
- Image color theming: derive subtle UI accents from dominant image color
- Offline‑first caching for last N images
- Minimal comments or “reactions” without clutter
- Theming toggle (system default, light/dark swap)
