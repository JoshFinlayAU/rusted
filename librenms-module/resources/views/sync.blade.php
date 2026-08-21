@extends('layouts.librenmsv1')

@section('title', 'Rusted — Bulk device sync')

@section('content')
<div class="container-fluid">
    <h2>
        <i class="fa fa-exchange"></i> Rusted &mdash; Bulk device sync
        <a href="{{ route('rusted.index') }}" class="btn btn-xs btn-default pull-right" title="Back to Rusted Backups">
            <i class="fa fa-arrow-left"></i> Back
        </a>
    </h2>

    <div id="sync-alerts"></div>

    <div class="row">
        <!-- LEFT: LibreNMS devices not actively backed up by rusted -->
        <div class="col-md-4">
            <input id="sync-left-filter" class="form-control input-sm" type="text"
                   placeholder="regex filter (hostname / os)">
            <div id="sync-left" class="list-group" style="max-height:500px;overflow:auto">
                <div class="list-group-item"><em>Loading&hellip;</em></div>
            </div>
        </div>

        <!-- CENTER: arrows + per-family driver/credential -->
        <div class="col-md-4" style="text-align:center;padding-top:40px">
            <div class="form-group">
                <label>Driver (applied to the whole batch)</label>
                <select id="sync-driver" class="form-control input-sm"></select>
            </div>
            <div class="form-group">
                <label>Credential</label>
                <select id="sync-credential" class="form-control input-sm"></select>
            </div>
            <button id="sync-add" class="btn btn-success btn-sm" style="margin-top:6px">
                <i class="fa fa-arrow-right"></i> Add to rusted
            </button>
            <button id="sync-disable" class="btn btn-danger btn-sm" style="margin-top:6px">
                <i class="fa fa-arrow-left"></i> Disable in rusted
            </button>
            <p class="text-muted" style="margin-top:10px;font-size:12px">
                Disabling a device stops backups but <strong>preserves its
                configuration history</strong>; it reappears here so it can be
                re-enabled. Devices that are in rusted but not in LibreNMS are
                shown greyed and locked.
            </p>
        </div>

        <!-- RIGHT: rusted devices also in LibreNMS (selectable) + orphans (greyed) -->
        <div class="col-md-4">
            <input id="sync-right-filter" class="form-control input-sm" type="text"
                   placeholder="regex filter (hostname / driver)">
            <div id="sync-right" class="list-group" style="max-height:500px;overflow:auto">
                <div class="list-group-item"><em>Loading&hellip;</em></div>
            </div>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
    };
    const driverSel = document.getElementById('sync-driver');
    const credSel = document.getElementById('sync-credential');

    // Hosts that failed the last add/disable, kept highlighted red until a later refresh.
    const errorSet = new Set();

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        const d = document.getElementById('sync-alerts');
        const div = document.createElement('div');
        div.className = 'alert alert-' + type + ' alert-dismissable';
        div.innerHTML = '<button type="button" class="close" data-dismiss="alert">&times;</button>' + esc(msg);
        d.appendChild(div);
        setTimeout(() => div.remove(), 10000);
    }

    async function apiJson(method, path, body) {
        const opts = { method, credentials: 'same-origin',
            headers: { 'Accept': 'application/json', 'X-Requested-With': 'XMLHttpRequest', 'X-CSRF-TOKEN': RUSTED.csrf } };
        if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
        const res = await fetch(RUSTED.base + path, opts);
        let data = null;
        try { data = await res.json(); } catch (e) { /* non-JSON */ }
        return { ok: res.ok, status: res.status, data: data };
    }

    function buildRegex(value) {
        value = (value || '').trim();
        if (!value) return null;
        try { return new RegExp(value, 'i'); } catch (e) { return null; }
    }

    function filterList(listId, re) {
        document.querySelectorAll('#' + listId + ' .list-group-item').forEach(item => {
            if (!re) { item.style.display = ''; return; }
            const txt = (item.getAttribute('data-host') || '') + ' ' + (item.getAttribute('data-meta') || '');
            item.style.display = re.test(txt) ? '' : 'none';
        });
    }

    document.getElementById('sync-left-filter').addEventListener('input', () => filterList('sync-left', buildRegex(document.getElementById('sync-left-filter').value)));
    document.getElementById('sync-right-filter').addEventListener('input', () => filterList('sync-right', buildRegex(document.getElementById('sync-right-filter').value)));

    function hostOf(item) { return item.getAttribute('data-host') || ''; }

    function renderLeft(items) {
        const box = document.getElementById('sync-left');
        if (!items || !items.length) {
            box.innerHTML = '<div class="list-group-item"><em>No LibreNMS devices to add.</em></div>';
            return;
        }
        box.innerHTML = items.map(d => {
            const h = esc(d.name);
            const rustNote = d.rust_state === 'disabled'
                ? '<span class="label label-info" title="Already in rusted but disabled — re-adding re-enables it">disabled</span> '
                : '';
            const flags = (d.disabled ? '<span class="label label-warning">disabled</span> ' : '')
                + (!d.status ? '<span class="label label-danger">down</span>' : '');
            return '<div class="list-group-item" data-host="' + esc(d.name) + '" data-meta="' + esc(d.os || '') + '"'
                + (d.disabled ? ' style="opacity:0.6"' : '') + '>'
                + '<input type="checkbox" class="sync-check"> '
                + '<strong>' + h + '</strong> ' + rustNote + flags
                + (d.os ? ' <span class="text-muted">' + esc(d.os) + '</span>' : '') + '</div>';
        }).join('');
        bindList('sync-left');
    }

    function renderRight(right, grey) {
        const box = document.getElementById('sync-right');
        const rw = Array.isArray(right) ? right : [];
        const gr = Array.isArray(grey) ? grey : [];
        if (!rw.length && !gr.length) {
            box.innerHTML = '<div class="list-group-item"><em>No devices managed by rusted yet.</em></div>';
            return;
        }
        const rows = rw.map(d => {
            const h = esc(d.name);
            const drv = esc(d.driver || '');
            return '<div class="list-group-item" data-host="' + h + '" data-meta="' + drv + '">'
                + '<input type="checkbox" class="sync-check"> '
                + '<strong>' + h + '</strong> <small class="text-muted">(' + drv + ')</small> '
                + '<a href="' + '{{ url('plugin/rusted/device') }}' + '/' + h 
                + '" class="btn btn-xs btn-default pull-right" title="View configuration pippo"><i class="fa fa-eye"></i></a>'
                + '</div>';
        });
        const greys = gr.map(d => {
            const h = esc(d.name);
            return '<div class="list-group-item list-group-item-default" data-host="' + h + '" data-meta="' + esc(d.driver || '') + '">'
                + '<input type="checkbox" class="sync-check" disabled> '
                + '<strong>' + h + '</strong> <small class="text-muted">(' + esc(d.driver || '') + ')</small> '
                + '<span class="text-muted" title="In rusted but not in LibreNMS — not selectable">ⓘ not in LibreNMS</span></div>';
        });
        box.innerHTML = rows.concat(greys).join('');
        bindList('sync-right');
    }

    // Click the row to toggle its checkbox (unless clicking the checkbox or a
    // link directly). Disabled (greyed) checkboxes never toggle.
    function bindList(listId) {
        const box = document.getElementById(listId);
        box.addEventListener('click', e => {
            if (e.target.closest('.sync-check')) return;     // native checkbox toggle
            if (e.target.closest('a')) return;               // let the link navigate
            const item = e.target.closest('.list-group-item');
            if (!item) return;
            const cb = item.querySelector('.sync-check');
            if (cb && !cb.disabled) cb.toggle();
        });
    }

    function applyErrors() {
        document.querySelectorAll('#sync-left .list-group-item, #sync-right .list-group-item').forEach(item => {
            item.classList.toggle('list-group-item-danger', errorSet.has(hostOf(item)));
        });
    }

    function selectedIn(listId) {
        return Array.from(document.querySelectorAll('#' + listId + ' .sync-check:checked'))
            .map(cb => hostOf(cb.closest('.list-group-item')));
    }

    async function loadDrivers() {
        const r = await apiJson('GET', '/drivers');
        const drivers = (r.ok && Array.isArray(r.data)) ? r.data : [];
        driverSel.innerHTML = '<option value="" disabled selected>driver</option>'
            + (drivers.map(d => '<option value="' + esc(d.name) + '">' + esc(d.name) + '</option>')).join('');
    }

    async function loadCredentials() {
        const r = await apiJson('GET', '/credentials');
        const creds = (r.ok && Array.isArray(r.data)) ? r.data : [];
        credSel.innerHTML = '<option value="" disabled selected>credential</option>'
            + creds.map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + ' (' + esc(c.username) + ')</option>').join('');
    }

    async function loadState() {
        const r = await apiJson('GET', '/sync/state');
        if (!r.ok) {
            alertBox('danger', 'Could not load sync state: ' + ((r.data && r.data.error) || ('HTTP ' + r.status)));
            return;
        }
        renderLeft(r.data.left || []);
        renderRight(r.data.right || [], r.data.grey || []);
        applyErrors();
    }

    document.getElementById('sync-add').addEventListener('click', async () => {
        const devices = selectedIn('sync-left');
        if (!devices.length) return alertBox('warning', 'Select devices from the left list.');
        const driver = driverSel.value;
        const credential = credSel.value;
        if (!driver) return alertBox('warning', 'Pick a driver.');
        if (!credential) return alertBox('warning', 'Pick a credential.');

        const btn = document.getElementById('sync-add');
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Adding&hellip;';
        const r = await apiJson('POST', '/sync/add', { driver: driver, credential: credential, devices: devices });
        btn.disabled = false;
        btn.innerHTML = '<i class="fa fa-arrow-right"></i> Add to rusted';

        if (r.ok && r.data) {
            if (r.data.failed && r.data.failed.length) {
                r.data.failed.forEach(f => errorSet.add(f.name));
                alertBox('danger', r.data.failed.length + ' device(s) failed to add; they stay on the left, highlighted red.');
            }
            if (r.data.added && r.data.added.length) {
                r.data.added.forEach(n => errorSet.delete(n));
                alertBox('success', 'Added ' + r.data.added.length + ' device(s) to rusted.');
            }
        } else {
            alertBox('danger', 'Add failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
        }
        loadState();
    });

    document.getElementById('sync-disable').addEventListener('click', async () => {
        const devices = selectedIn('sync-right');
        if (!devices.length) return alertBox('warning', 'Select devices from the right list (greyed rows are not selectable).');

        const msg = 'Disable ' + devices.length + ' device(s) in rusted?\n\n'
            + 'This stops backups for them but preserves their configuration\n'
            + 'history and keeps them in rusted. Disabled devices reappear on the\n'
            + 'left list and can be re-enabled later.\n\n'
            + 'Confirm to proceed.';
        if (!confirm(msg)) return;

        const btn = document.getElementById('sync-disable');
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Disabling&hellip;';
        const r = await apiJson('POST', '/sync/disable', { devices: devices });
        btn.disabled = false;
        btn.innerHTML = '<i class="fa fa-arrow-left"></i> Disable in rusted';

        if (r.ok && r.data) {
            if (r.data.kept && r.data.kept.length) {
                r.data.kept.forEach(k => errorSet.add(k.name));
                alertBox('warning', r.data.kept.length + ' device(s) could not be disabled and remain managed.');
            }
            if (r.data.disabled && r.data.disabled.length) {
                r.data.disabled.forEach(n => errorSet.delete(n));
                alertBox('success', 'Disabled ' + r.data.disabled.length + ' device(s). Their history is preserved in rusted.');
            }
        } else {
            alertBox('danger', 'Disable failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
        }
        loadState();
    });

    // Initial load.
    loadDrivers();
    loadCredentials();
    loadState();
})();
</script>
@endsection
