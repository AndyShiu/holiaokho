(function () {
  var html = document.documentElement;
  html.classList.add('js');

  // The head already applied ?theme= or the stored choice before first paint;
  // this only carries a forced theme across the language links so a preview
  // stays on the scheme it was asked for.
  var theme = new URLSearchParams(location.search).get('theme');
  if (theme === 'dark' || theme === 'light') {
    document.querySelectorAll('a[data-lang]').forEach(function (a) {
      a.href += (a.href.indexOf('?') > -1 ? '&' : '?') + 'theme=' + theme;
    });
  }

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

  // Language. A static host cannot see where a visitor is, so this goes by the
  // browser's own language preference — in practice the same thing for the
  // audience this is aimed at. Traditional Chinese readers land on the zh-TW
  // page; everyone else stays on English.
  //
  // Three things this must not do: trap someone who wants the other language,
  // bounce between the two pages, or redirect a crawler (they report English,
  // so they stay put, and hreflang declares both versions anyway).
  var LANG_KEY = 'holiaokho.lang';
  var onZh = /index\.zh-TW\.html$/.test(location.pathname);

  // Following a language link is an explicit choice; remember it and never
  // override it again.
  document.querySelectorAll('a[data-lang]').forEach(function (a) {
    a.addEventListener('click', function () {
      try { localStorage.setItem(LANG_KEY, onZh ? 'en' : 'zh-TW'); } catch (e) {}
    });
  });

  var chosen = null;
  try { chosen = localStorage.getItem(LANG_KEY); } catch (e) {}
  if (!chosen && !onZh && /^zh-(Hant|TW|HK|MO)\b/i.test(navigator.language || '')) {
    location.replace('index.zh-TW.html' + location.search + location.hash);
    return;
  }

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
