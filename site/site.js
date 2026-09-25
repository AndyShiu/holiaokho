(function () {
  var html = document.documentElement;
  html.classList.add('js');

  // The head already applied ?theme= or the stored choice before first paint;
  // this only carries a forced theme across the language links so a preview
  // stays on the scheme it was asked for.
  // Theme toggle. Without a stored choice the page follows the system, so the
  // first click has to resolve what is actually on screen rather than read an
  // attribute that is not there yet.
  var THEME_KEY = 'holiaokho.theme';
  document.querySelectorAll('button.theme').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var current = html.getAttribute('data-theme');
      if (!current) {
        current = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
      }
      var next = current === 'dark' ? 'light' : 'dark';
      html.setAttribute('data-theme', next);
      try { localStorage.setItem(THEME_KEY, next); } catch (e) {}
    });
  });

  // Language. The head already chose one; these buttons record a deliberate
  // choice so the browser's preference never overrides it again.
  document.querySelectorAll('button[data-set-lang]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var l = btn.getAttribute('data-set-lang');
      html.setAttribute('data-lang', l);
      html.lang = l === 'zh' ? 'zh-Hant-TW' : 'en';
      try { localStorage.setItem('holiaokho.lang', l); } catch (e) {}
    });
  });

  // copy buttons
  document.querySelectorAll('button[data-copy]').forEach(function (btn) {
    var idle = btn.textContent, done = btn.getAttribute('data-done-label') || 'Copied';
    btn.addEventListener('click', function () {
      var el = document.getElementById(btn.getAttribute('data-copy'));
      if (!el) return;
      var text = el.textContent.trim();
      var finish = function () {
        btn.textContent = done; btn.setAttribute('data-done', '1');
        setTimeout(function () { btn.textContent = idle; btn.removeAttribute('data-done'); }, 1600);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(finish, function () { fallback(text); finish(); });
      } else { fallback(text); finish(); }
    });
  });
  function fallback(text) {
    var ta = document.createElement('textarea');
    ta.value = text; ta.setAttribute('readonly', ''); ta.style.position = 'fixed'; ta.style.opacity = '0';
    document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); } catch (e) {}
    document.body.removeChild(ta);
  }

  // Quick-start tabs. The markup shows both panels; with JS, one at a time.
  var tabs = document.querySelectorAll('.tabs [data-tab]');
  var show = function (name) {
    tabs.forEach(function (t) { t.setAttribute('aria-selected', t.getAttribute('data-tab') === name ? 'true' : 'false'); });
    document.querySelectorAll('.tab-panel').forEach(function (p) { p.hidden = p.getAttribute('data-panel') !== name; });
  };
  tabs.forEach(function (t) { t.addEventListener('click', function () { show(t.getAttribute('data-tab')); }); });
  if (tabs.length) show('compose');

  // The version in the corner comes from the latest release, so the page is
  // never behind the product. The number in the markup is the fallback.
  var ver = document.getElementById('ver');
  if (ver && window.fetch) {
    fetch('https://api.github.com/repos/AndyShiu/holiaokho/releases/latest', { headers: { Accept: 'application/vnd.github+json' } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (rel) {
        if (rel && rel.tag_name) { ver.textContent = rel.tag_name; if (rel.html_url) ver.href = rel.html_url; }
      })
      .catch(function () {});
  }

  // section fade-in, skipped under reduced motion
  var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var els = document.querySelectorAll('.reveal');
  if (reduce || !('IntersectionObserver' in window)) {
    els.forEach(function (e) { e.classList.add('in'); });
    return;
  }
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (en) {
      if (en.isIntersecting) { en.target.classList.add('in'); io.unobserve(en.target); }
    });
  }, { rootMargin: '0px 0px -8% 0px' });
  els.forEach(function (e) { io.observe(e); });
})();
