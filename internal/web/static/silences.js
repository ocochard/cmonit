// Shows alert silences on host pages. Text is inserted with textContent only.
(function () {
    'use strict';
    const API = '/api/alerts/silences';
    const DURATIONS = [['1 hour', 3600], ['4 hours', 14400], ['24 hours', 86400], ['Until removed', 0]];

    function globRe(pattern) {
        const src = pattern.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.');
        return new RegExp('^' + src + '$');
    }
    const hostMatches = (s, hostname) => !s.host_pattern || s.host_pattern === '*' || globRe(s.host_pattern).test(hostname);
    const wholeHost = (s) => !s.service_pattern || s.service_pattern === '*';

    function el(tag, cls, text) {
        const e = document.createElement(tag);
        if (cls) e.className = cls;
        if (text !== undefined) e.textContent = text;
        return e;
    }

    function until(s) {
        return s.expires_at ? 'until ' + new Date(s.expires_at * 1000).toLocaleString() : 'until removed';
    }

    async function load() {
        try {
            const r = await fetch(API);
            return r.ok ? await r.json() : [];
        } catch (e) {
            return [];
        }
    }

    // Host detail page: one control per host header.
    function renderControl(box, silences) {
        const hostname = box.dataset.hostname;
        const mine = silences.filter(s => hostMatches(s, hostname));
        box.replaceChildren();

        const own = mine.filter(s => s.host_pattern === hostname && wholeHost(s));
        const other = mine.filter(s => !own.includes(s));

        for (const s of other) {
            const scope = wholeHost(s) ? '' : ' (' + s.service_pattern + ')';
            const b = el('span', 'px-3 py-1 rounded-full text-xs font-semibold bg-yellow-100 text-yellow-800',
                'Silenced by ' + s.host_pattern + scope + ' ' + until(s));
            b.title = s.reason || '';
            box.append(b);
        }
        if (own.length) {
            const s = own[0];
            const b = el('span', 'px-3 py-1 rounded-full text-xs font-semibold bg-yellow-100 text-yellow-800', 'Alerts silenced ' + until(s));
            b.title = s.reason || '';
            const rm = el('button', 'bg-blue-800 hover:bg-blue-900 px-3 py-1 rounded text-sm font-semibold', 'Turn alerts on');
            rm.addEventListener('click', async () => {
                for (const x of own) await fetch(API + '/' + x.id, {method: 'DELETE'});
                refresh();
            });
            box.append(b, rm);
            return;
        }
        const sel = el('select', 'bg-blue-800 text-white rounded px-2 py-1 text-sm');
        for (const [label, secs] of DURATIONS) {
            const o = el('option', '', label);
            o.value = secs;
            sel.append(o);
        }
        const btn = el('button', 'bg-blue-800 hover:bg-blue-900 px-3 py-1 rounded text-sm font-semibold', 'Silence alerts');
        btn.addEventListener('click', async () => {
            const r = await fetch(API, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({host_pattern: hostname, service_pattern: '*', duration_seconds: Number(sel.value), reason: 'silenced from host page'}),
            });
            if (!r.ok) alert('Failed to silence alerts: ' + ((await r.json().catch(() => ({}))).error || 'HTTP ' + r.status));
            refresh();
        });
        box.append(sel, btn);
    }

    // Overview page: small badge in the hostname cell of silenced hosts.
    function renderBadge(row, silences) {
        const cell = row.querySelector('td[data-host]');
        if (!cell) return;
        const old = cell.querySelector('.silence-badge');
        if (old) old.remove();
        const hit = silences.find(s => hostMatches(s, row.dataset.hostname) && wholeHost(s));
        if (!hit) return;
        const b = el('span', 'silence-badge ml-2 px-2 py-0.5 rounded-full text-xs font-semibold bg-yellow-100 text-yellow-800', 'alerts silenced');
        b.title = until(hit) + (hit.reason ? ' - ' + hit.reason : '');
        cell.append(b);
    }

    async function refresh() {
        const silences = await load();
        document.querySelectorAll('.silence-control').forEach(box => renderControl(box, silences));
        document.querySelectorAll('tr.host-row').forEach(row => renderBadge(row, silences));
    }

    refresh();
})();
