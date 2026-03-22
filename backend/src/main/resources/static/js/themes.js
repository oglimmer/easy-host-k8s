(function () {
    var THEMES = [
        { id: 'obsidian',   label: 'Obsidian',   swatch: '#FF4F38', bg: '#0B0B0F' },
        { id: 'paper',      label: 'Paper',      swatch: '#C4533A', bg: '#FAF7F2' },
        { id: 'terminal',   label: 'Terminal',   swatch: '#33FF33', bg: '#050505' },
        { id: 'bauhaus',    label: 'Bauhaus',    swatch: '#E63329', bg: '#FFFFFF' },
        { id: 'nord',       label: 'Nord',       swatch: '#88C0D0', bg: '#2E3440' },
        { id: 'cyberpunk',  label: 'Cyberpunk',  swatch: '#FF2D95', bg: '#0D001A' },
        { id: 'solarized',  label: 'Solarized',  swatch: '#268BD2', bg: '#FDF6E3' },
        { id: 'concrete',   label: 'Concrete',   swatch: '#111111', bg: '#FFFFFF' }
    ];

    function getTheme() {
        return localStorage.getItem('eh-theme') || 'obsidian';
    }

    function setTheme(id) {
        document.documentElement.dataset.theme = id;
        localStorage.setItem('eh-theme', id);
        updateActive(id);
    }

    function updateActive(id) {
        var btns = document.querySelectorAll('.ts__btn');
        for (var i = 0; i < btns.length; i++) {
            btns[i].classList.toggle('ts__btn--active', btns[i].dataset.theme === id);
        }
    }

    function createSwitcher() {
        var wrap = document.createElement('div');
        wrap.className = 'ts';

        var toggle = document.createElement('button');
        toggle.className = 'ts__toggle';
        toggle.setAttribute('aria-label', 'Switch theme');
        toggle.innerHTML = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.5"/><path d="M8 2a6 6 0 0 0 0 12V2z" fill="currentColor"/></svg>';
        wrap.appendChild(toggle);

        var panel = document.createElement('div');
        panel.className = 'ts__panel';

        var current = getTheme();
        for (var i = 0; i < THEMES.length; i++) {
            var t = THEMES[i];
            var btn = document.createElement('button');
            btn.className = 'ts__btn' + (t.id === current ? ' ts__btn--active' : '');
            btn.dataset.theme = t.id;
            btn.innerHTML =
                '<span class="ts__swatch" style="background:' + t.bg + ';border-color:' + t.swatch + '">' +
                '<span style="background:' + t.swatch + '"></span></span>' +
                '<span class="ts__label">' + t.label + '</span>';
            btn.addEventListener('click', (function (id) {
                return function () { setTheme(id); };
            })(t.id));
            panel.appendChild(btn);
        }

        wrap.appendChild(panel);

        toggle.addEventListener('click', function (e) {
            e.stopPropagation();
            wrap.classList.toggle('ts--open');
        });

        document.addEventListener('click', function (e) {
            if (!wrap.contains(e.target)) {
                wrap.classList.remove('ts--open');
            }
        });

        document.body.appendChild(wrap);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', createSwitcher);
    } else {
        createSwitcher();
    }
})();
