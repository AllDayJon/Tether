// ============================================
// Tether Website — JavaScript
// ============================================

// ── Copy buttons ──────────────────────────────────────────────
document.querySelectorAll('.copy-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    const targetId = btn.dataset.target;
    const codeEl = document.getElementById(targetId);
    if (!codeEl) return;

    navigator.clipboard.writeText(codeEl.textContent.trim()).then(() => {
      btn.textContent = 'Copied!';
      btn.classList.add('copied');
      setTimeout(() => {
        btn.textContent = 'Copy';
        btn.classList.remove('copied');
      }, 1800);
    });
  });
});

// ── GitHub star count + latest version ───────────────────────
fetch('https://api.github.com/repos/AllDayJon/Tether')
  .then(r => r.json())
  .then(data => {
    if (data.stargazers_count != null) {
      const count = data.stargazers_count >= 1000
        ? (data.stargazers_count / 1000).toFixed(1) + 'k'
        : String(data.stargazers_count);
      const el = document.getElementById('star-count');
      if (el) el.textContent = count;
    }
  })
  .catch(() => {}); // silently ignore if repo doesn't exist yet

fetch('https://api.github.com/repos/AllDayJon/Tether/releases/latest')
  .then(r => r.json())
  .then(data => {
    if (!data.tag_name) return;
    document.querySelectorAll('.version-badge').forEach(el => {
      el.textContent = data.tag_name;
    });
  })
  .catch(() => {});

// ── FAQ accordion ─────────────────────────────────────────────
document.querySelectorAll('.faq-q').forEach(btn => {
  btn.addEventListener('click', () => {
    const item = btn.closest('.faq-item');
    const isOpen = item.classList.contains('open');
    // Close all
    document.querySelectorAll('.faq-item.open').forEach(el => {
      el.classList.remove('open');
      el.querySelector('.faq-q').setAttribute('aria-expanded', 'false');
    });
    // Open clicked (unless it was already open)
    if (!isOpen) {
      item.classList.add('open');
      btn.setAttribute('aria-expanded', 'true');
    }
  });
});

// ── Smooth active nav link ────────────────────────────────────
const sections = document.querySelectorAll('section[id]');
const navLinks = document.querySelectorAll('.nav-links a[href^="#"]');

const observer = new IntersectionObserver(entries => {
  entries.forEach(entry => {
    if (entry.isIntersecting) {
      const id = entry.target.id;
      navLinks.forEach(link => {
        link.style.color = link.getAttribute('href') === `#${id}`
          ? 'var(--text)'
          : '';
      });
    }
  });
}, { rootMargin: '-40% 0px -55% 0px' });

sections.forEach(s => observer.observe(s));

// ── Scroll-in animations ──────────────────────────────────────
const animateOnScroll = new IntersectionObserver(entries => {
  entries.forEach(entry => {
    if (entry.isIntersecting) {
      entry.target.classList.add('visible');
      animateOnScroll.unobserve(entry.target);
    }
  });
}, { threshold: 0.08 });

document.querySelectorAll(
  '.how-card, .mode-card, .feature-card, .install-step, .rule-item, .cmd-group'
).forEach((el, i) => {
  el.style.opacity = '0';
  el.style.transform = 'translateY(18px)';
  el.style.transition = `opacity 0.45s ease ${i * 0.06}s, transform 0.45s ease ${i * 0.06}s`;
  el.classList.add('anim-target');
  animateOnScroll.observe(el);
});

document.addEventListener('animationend', () => {}, true);

// Inject .visible state
const style = document.createElement('style');
style.textContent = '.anim-target.visible { opacity: 1 !important; transform: translateY(0) !important; }';
document.head.appendChild(style);
