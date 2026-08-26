@extends('layouts.librenmsv1')

@section('title', 'Rusted — ' . $hostname)

@section('content')
<div class="container-fluid">
    <h2>
        <i class="fa fa-floppy-o"></i> Rusted &mdash; <span id="rusted-dev-title">{{ $hostname }}</span>
    </h2>

    <div id="rusted-alerts"></div>

    <div class="text-muted" id="rusted-meta" style="margin-bottom:12px">
        <em>Loading device&hellip;</em>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Latest configuration</strong>
            <span class="pull-right">
                <button id="rusted-config-refresh" class="btn btn-xs btn-default"><i class="fa fa-refresh"></i></button>
                <a id="rusted-config-history" class="btn btn-xs btn-default" title="Download all config revisions as a zip" href="#"><i class="fa fa-download"></i> All revisions</a>
                <button id="rusted-config-download" class="btn btn-xs btn-default"><i class="fa fa-download"></i> Raw</button>
            </span>
        </div>
        <div class="panel-body">
            <pre id="rusted-config" style="max-height:500px;overflow:auto;margin:0"><em>Loading&hellip;</em></pre>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Revision history</strong>
            <button id="rusted-versions-refresh" class="btn btn-xs btn-default pull-right"><i class="fa fa-refresh"></i></button>
        </div>
        <div class="panel-body">
            <table class="table table-condensed table-striped" style="margin-bottom:0">
                <thead>
                    <tr><th>Commit</th><th>Date</th><th>Subject</th><th class="text-right">Actions</th></tr>
                </thead>
                <tbody id="rusted-versions">
                    <tr><td colspan="4"><em>Loading&hellip;</em></td></tr>
                </tbody>
            </table>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Compare revisions</strong>
        </div>
        <div class="panel-body">
            <div class="form-inline" style="margin-bottom:8px">
                <select class="form-control input-sm" id="rusted-diff-from" style="width:180px"></select>
                <select class="form-control input-sm" id="rusted-diff-to" style="width:180px"></select>
                <button id="rusted-diff-swap" class="btn btn-xs btn-default" type="button" style="margin-left:6px">⇅</button>
                <button id="rusted-diff-run" class="btn btn-xs btn-primary" type="button" style="margin-left:6px">Diff</button>
            </div>
            <pre id="rusted-diff" style="max-height:500px;overflow:auto;margin:0;background:#fff"><em>Select two revisions and pick Diff.</em></pre>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
        host: @json($hostname),
    };
    const host = encodeURIComponent(RUSTED.host);

    const $alerts = document.getElementById('rusted-alerts');
    const $meta = document.getElementById('rusted-meta');
    const $title = document.getElementById('rusted-dev-title');
    const $config = document.getElementById('rusted-config');
    const $diff = document.getElementById('rusted-diff');
    const $versions = document.getElementById('rusted-versions');
    const $from = document.getElementById('rusted-diff-from');
    const $to = document.getElementById('rusted-diff-to');
    const $swap = document.getElementById('rusted-diff-swap');

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        const div = document.createElement('div');
        div.className = 'alert alert-' + type + ' alert-dismissable';
        div.innerHTML = '<button type="button" class="close" data-dismiss="alert">&times;</button>' + esc(msg);
        $alerts.appendChild(div);
        setTimeout(() => div.remove(), 8000);
    }

    async function apiText(method, path, query) {
        const url = RUSTED.base + path + (query ? '?' + query : '');
        const res = await fetch(url, {
            method: method,
            credentials: 'same-origin',
            headers: { 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-TOKEN': RUSTED.csrf },
        });
        const text = await res.text();
        return { ok: res.ok, status: res.status, text: text };
    }

    async function apiJson(method, path, body) {
        const opts = { method: method, credentials: 'same-origin',
            headers: { 'Accept': 'application/json', 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-TOKEN': RUSTED.csrf } };
        if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
        const res = await fetch(RUSTED.base + path, opts);
        let data = null;
        try { data = await res.json(); } catch (e) { /* non-JSON */ }
        return { ok: res.ok, status: res.status, data: data };
    }

    async function loadDevice() {
        $meta.innerHTML = '<em>Loading&hellip;</em>';
        const r = await apiJson('GET', '/devices/' + host);
        if (r.status === 404) {
            $meta.innerHTML = '<div class="text-warning">This device is not managed by rusted. '
                + '<a href="' + @json(url('plugin/rusted')) + '">Open the Rusted Backups page</a> to add it.</div>';
            $config.textContent = '(not managed)';
            return;
        }
        if (!r.ok) {
            $meta.innerHTML = '<div class="text-danger">Rusted API error: ' + esc((r.data && r.data.error) || ('HTTP ' + r.status)) + '</div>';
            $config.textContent = '(error)';
            return;
        }
        const d = r.data || {};
        $title.textContent = RUSTED.host;
        $meta.innerHTML =
            'Driver: <code>' + esc(d.driver || '—') + '</code>'
            + (d.group ? ' · Group: <code>' + esc(d.group) + '</code>' : '')
            + ' · Enabled: <code>' + (d.enabled ? 'yes' : 'no') + '</code>';
        loadVersions();
        loadConfig();
    }

    async function loadConfig(commit) {
        $config.innerHTML = '<em>Loading&hellip;</em>';
        let query = '';
        if (commit) query = 'commit=' + encodeURIComponent(commit);
        const r = await apiText('GET', '/devices/' + host + '/config', query);
        if (!r.ok) {
            $config.textContent = (r.status === 404) ? '(no backup yet)' : ('error: HTTP ' + r.status);
            return;
        }
        $config.textContent = r.text === '' ? '(empty)' : r.text;
    }

    async function loadVersions() {
        $versions.innerHTML = '<tr><td colspan="4"><em>Loading&hellip;</em></td></tr>';
        $from.innerHTML = '<option value="" disabled selected>older revision</option>';
        $to.innerHTML = '<option value="" disabled selected>newer revision</option>';
        const r = await apiJson('GET', '/devices/' + host + '/versions');
        if (!r.ok) {
            $versions.innerHTML = '<tr><td colspan="4"><em>Could not load revisions.</em></td></tr>';
            return;
        }
        const versions = (r.status === 404 || !Array.isArray(r.data)) ? [] : r.data;
        if (!versions.length) {
            $versions.innerHTML = '<tr><td colspan="4"><em>No revisions yet.</em></td></tr>';
            return;
        }
        // Newest first. Populate history table.
        $versions.innerHTML = versions.map(function (v) {
            const short = (v.commit || '').substring(0, 8);
            return '<tr>'
                + '<td><code>' + esc(short) + '</code></td>'
                + '<td>' + esc(v.date) + '</td>'
                + '<td>' + esc(v.subject) + '</td>'
                + '<td class="text-right">'
                + '<button class="btn btn-xs btn-default" data-commit="' + esc(v.commit) + '" title="View this revision"><i class="fa fa-eye"></i></button> '
                + '<button class="btn btn-xs btn-default" data-diff="' + esc(v.commit) + '" title="Diff from this revision"><i class="fa fa-exchange"></i></button>'
                + '</td></tr>';
        }).join('');

        // Populate diff selects: newest first so the default "latest change" is natural.
        versions.forEach(function (v) {
            const short = (v.commit || '').substring(0, 8);
            const opt = document.createElement('option');
            opt.value = v.commit;
            opt.textContent = short + ' — ' + (v.subject || '') + ' (' + v.date + ')';
            $from.appendChild(opt.cloneNode(true));
            $to.appendChild(opt);
        });
        // Default: diff the latest commit against its parent (single-revision change).
        $from.value = versions[0].commit;
        $to.value = '';
    }

    async function loadDiff() {
        const from = $from.value;
        const to = $to.value;
        $diff.innerHTML = '<em>Computing diff&hellip;</em>';
        let query = '';
        if (from) query += 'from=' + encodeURIComponent(from);
        if (to) query += (query ? '&' : '') + 'to=' + encodeURIComponent(to);
        const r = await apiText('GET', '/devices/' + host + '/diff', query);
        if (!r.ok) {
            $diff.textContent = (r.status === 404) ? '(no such revision)' : ('error: HTTP ' + r.status);
            return;
        }
        renderDiff(r.text);
    }

    function renderDiff(text) {
        if (!text) { $diff.textContent = '(no diff)'; return; }
        const lines = String(text).split('\n');
        $diff.innerHTML = lines.map(function (l) {
            const c = esc(l);
            if (l.startsWith('diff ') || l.startsWith('index ') || l.startsWith('+++') || l.startsWith('---'))
                return '<span class="text-muted">' + c + '</span>';
            if (l.startsWith('@@')) return '<span class="text-info">' + c + '</span>';
            if (l.startsWith('+')) return '<span class="text-success">' + c + '</span>';
            if (l.startsWith('-')) return '<span class="text-danger">' + c + '</span>';
            return c;
        }).join('\n');
    }

    async function doBackup() {
        const r = await apiJson('POST', '/devices/' + host + '/backup');
        if (!r.ok) {
            alertBox('danger', 'Backup failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
            return;
        }
        alertBox('success', 'Backup OK (' + (r.data && r.data.status) + ')');
        loadVersions();
    }

    function downloadConfig(text) {
        const blob = new Blob([text], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = RUSTED.host + '.cfg';
        a.click();
        URL.revokeObject(url);
    }

    // "Back up now"
    document.getElementById('rusted-config-refresh').addEventListener('click', () => loadConfig());
    document.getElementById('rusted-config-download').addEventListener('click', () => downloadConfig($config.textContent));
    document.getElementById('rusted-config-history').addEventListener('click', e => {
        e.preventDefault();
        window.location.href = RUSTED.base + '/devices/' + host + '/history/zip';
    });

    document.getElementById('rusted-versions-refresh').addEventListener('click', loadVersions);

    $swap.addEventListener('click', () => {
        const tmp = $from.value; $from.value = $to.value; $to.value = tmp;
        if ($from.value) loadDiff();
    });
    document.getElementById('rusted-diff-run').addEventListener('click', loadDiff);

    // Per-revision actions via event delegation.
    $versions.addEventListener('click', function (e) {
        const eye = e.target.closest('button[data-commit]');
        if (eye) { loadConfig(eye.getAttribute('data-commit')); return; }
        const diffBtn = e.target.closest('button[data-diff]');
        if (diffBtn) {
            $from.value = diffBtn.getAttribute('data-diff');
            $to.value = '';
            loadDiff();
        }
    });

    // Top toolbar backup action.
    const $backupBtn = document.createElement('button');
    $backupBtn.className = 'btn btn-xs btn-primary';
    $backupBtn.innerHTML = '<i class="fa fa-download"></i> Back up now';
    $backupBtn.onclick = doBackup;
    $meta.appendChild($backupBtn);

    loadDevice();
})();
</script>
@endsection
