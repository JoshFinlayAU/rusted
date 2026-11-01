@extends('layouts.librenmsv1')

@section('title', 'Rusted — Device sync')

@section('content')
<div class="container-fluid">
    <h2>
        <i class="fa fa-exchange"></i> Rusted &mdash; Device sync
        <a href="{{ route('rusted.index') }}" class="btn btn-xs btn-default pull-right" title="Back to Rusted Backups">
            <i class="fa fa-arrow-left"></i> Back
        </a>
    </h2>

    @include('rusted::partials.sticky-alerts', ['id' => 'sync-alerts'])

    <div class="row">
        {{-- LEFT: LibreNMS devices not actively backed up by rusted --}}
        <div class="col-md-4">
            <h4>In LibreNMS, not backed up</h4>
            <input id="sync-left-filter" class="form-control input-sm" type="text"
                   placeholder="regex filter (name / hostname / os)">
            <div id="sync-left" class="list-group" style="max-height:500px;overflow:auto">
                <div class="list-group-item"><em>Loading&hellip;</em></div>
            </div>
        </div>

        {{-- CENTER: batch driver/credential + actions --}}
        <div class="col-md-4" style="text-align:center;padding-top:40px">
            <div class="form-group">
                <label for="sync-driver">Driver (applied to the whole batch)</label>
                <select id="sync-driver" class="form-control input-sm"></select>
            </div>
            <div class="form-group">
                <label for="sync-credential">Credential</label>
                <select id="sync-credential" class="form-control input-sm"></select>
            </div>
            <button id="sync-add" class="btn btn-success btn-sm" type="button" style="margin-top:6px">
                <i class="fa fa-arrow-right"></i> Add to rusted
            </button>
            <button id="sync-disable" class="btn btn-danger btn-sm" type="button" style="margin-top:6px">
                <i class="fa fa-arrow-left"></i> Disable in rusted
            </button>
            <p class="text-muted" style="margin-top:10px;font-size:12px">
                Disabling a device stops backups but <strong>preserves its
                configuration history</strong>; it reappears on the left so it can be
                re-enabled. Devices that are in rusted but not in LibreNMS are
                shown greyed and locked.
            </p>
        </div>

        {{-- RIGHT: enabled rusted devices also in LibreNMS + orphans (greyed) --}}
        <div class="col-md-4">
            <h4>Backed up by rusted</h4>
            <input id="sync-right-filter" class="form-control input-sm" type="text"
                   placeholder="regex filter (name / host / driver)">
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
        device: @json(url('plugin/rusted/device')),
        csrf: @json(csrf_token()),
    };
    const $alerts = document.getElementById('sync-alerts');
    const $left = document.getElementById('sync-left');
    const $right = document.getElementById('sync-right');
    const $leftFilter = document.getElementById('sync-left-filter');
    const $rightFilter = document.getElementById('sync-right-filter');
    const driverSel = document.getElementById('sync-driver');
    const credSel = document.getElementById('sync-credential');

    // Hosts that failed the last add/disable; highlighted red until they succeed.
    const errorSet = new Set();

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
    }

    function alertBox(type, msg) {
        window.rustedAlert($alerts, type, msg);
    }

    function errorOf(r) {
        return (r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status);
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

    function buildRegex(value) {
        value = (value || '').trim();
        if (!value) return null;
        try { return new RegExp(value, 'i'); } catch (e) { return null; }
    }

    function filterList($list, $input) {
        const re = buildRegex($input.value);
        $list.querySelectorAll('.list-group-item[data-host]').forEach(item => {
            const txt = item.getAttribute('data-host') + ' ' + (item.getAttribute('data-meta') || '');
            item.style.display = !re || re.test(txt) ? '' : 'none';
        });
    }

    $leftFilter.addEventListener('input', () => filterList($left, $leftFilter));
    $rightFilter.addEventListener('input', () => filterList($right, $rightFilter));

    function renderLeft(items) {
        if (!items.length) {
            $left.innerHTML = '<div class="list-group-item"><em>No LibreNMS devices to add.</em></div>';
            return;
        }
        $left.innerHTML = items.map(d => {
            const rustNote = d.rust_state === 'disabled'
                ? '<span class="label label-info" title="Already in rusted but disabled; adding re-enables it">disabled in rusted</span> '
                : '';
            const flags = (d.disabled ? '<span class="label label-warning">disabled</span> ' : '')
                + (!d.status ? '<span class="label label-danger">down</span>' : '');
            const hostNote = d.hostname && d.hostname !== d.name
                ? ' <small class="text-muted">' + esc(d.hostname) + '</small>'
                : '';
            return '<label class="list-group-item" data-id="' + esc(d.id) + '" data-host="' + esc(d.name) + '"'
                + ' data-meta="' + esc((d.hostname || '') + ' ' + (d.os || '')) + '"'
                + ' style="font-weight:normal;cursor:pointer' + (d.disabled ? ';opacity:0.6' : '') + '">'
                + '<input type="checkbox" class="sync-check"> '
                + '<strong>' + esc(d.name) + '</strong> ' + rustNote + flags + hostNote
                + (d.os ? ' <span class="text-muted">' + esc(d.os) + '</span>' : '') + '</label>';
        }).join('');
    }

    function renderRight(right, grey) {
        if (!right.length && !grey.length) {
            $right.innerHTML = '<div class="list-group-item"><em>No devices managed by rusted yet.</em></div>';
            return;
        }
        const rows = right.map(d => {
            const href = RUSTED.device + '/' + encodeURIComponent(d.name);
            return '<label class="list-group-item" data-host="' + esc(d.name) + '" data-meta="' + esc((d.host || '') + ' ' + (d.driver || '')) + '"'
                + ' style="font-weight:normal;cursor:pointer">'
                + '<input type="checkbox" class="sync-check"> '
                + '<strong>' + esc(d.name) + '</strong> <small class="text-muted">(' + esc(d.driver || '') + ')</small> '
                + '<a href="' + esc(href) + '" class="btn btn-xs btn-default pull-right" title="View configuration"><i class="fa fa-eye"></i></a>'
                + '</label>';
        });
        const greys = grey.map(d => {
            return '<div class="list-group-item text-muted" data-host="' + esc(d.name) + '" data-meta="' + esc(d.driver || '') + '">'
                + '<input type="checkbox" disabled> '
                + '<strong>' + esc(d.name) + '</strong> <small>(' + esc(d.driver || '') + ')</small> '
                + '<span title="In rusted but not in LibreNMS; not selectable">not in LibreNMS</span></div>';
        });
        $right.innerHTML = rows.concat(greys).join('');
    }

    function applyErrors() {
        [$left, $right].forEach($list => {
            $list.querySelectorAll('.list-group-item[data-host]').forEach(item => {
                item.classList.toggle('list-group-item-danger', errorSet.has(item.getAttribute('data-host')));
            });
        });
    }

    function selectedIn($list) {
        return Array.from($list.querySelectorAll('.sync-check:checked'))
            .map(cb => cb.closest('.list-group-item').getAttribute('data-host'));
    }

    function selectedIdsIn($list) {
        return Array.from($list.querySelectorAll('.sync-check:checked'))
            .map(cb => Number(cb.closest('.list-group-item').getAttribute('data-id')));
    }

    async function loadDrivers() {
        const r = await apiJson('GET', '/drivers');
        const drivers = (r.ok && Array.isArray(r.data)) ? r.data : [];
        driverSel.innerHTML = '<option value="" disabled selected>driver</option>'
            + drivers.map(d => '<option value="' + esc(d.name) + '">' + esc(d.name) + '</option>').join('');
    }

    async function loadCredentials() {
        const r = await apiJson('GET', '/credentials');
        const creds = (r.ok && Array.isArray(r.data)) ? r.data : [];
        credSel.innerHTML = '<option value="" disabled selected>credential</option>'
            + creds.map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + ' (' + esc(c.username) + ')</option>').join('');
    }

    async function loadState() {
        const r = await apiJson('GET', '/sync/state');
        if (!r.ok || !r.data) {
            alertBox('danger', 'Could not load sync state: ' + errorOf(r));
            return;
        }
        renderLeft(Array.isArray(r.data.left) ? r.data.left : []);
        renderRight(Array.isArray(r.data.right) ? r.data.right : [], Array.isArray(r.data.grey) ? r.data.grey : []);
        filterList($left, $leftFilter);
        filterList($right, $rightFilter);
        applyErrors();
    }

    function failureList(items) {
        return items.map(f => f.name + ': ' + f.error).join('\n');
    }

    document.getElementById('sync-add').addEventListener('click', async function () {
        const btn = this;
        const devices = selectedIdsIn($left);
        if (!devices.length) return alertBox('warning', 'Select devices from the left list.');
        if (!driverSel.value) return alertBox('warning', 'Pick a driver.');
        if (!credSel.value) return alertBox('warning', 'Pick a credential.');

        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Adding&hellip;';
        const r = await apiJson('POST', '/sync/add', { driver: driverSel.value, credential: credSel.value, devices: devices });
        btn.disabled = false;
        btn.innerHTML = '<i class="fa fa-arrow-right"></i> Add to rusted';

        if (r.ok && r.data) {
            const failed = r.data.failed || [];
            const added = r.data.added || [];
            failed.forEach(f => errorSet.add(f.name));
            added.forEach(n => errorSet.delete(n));
            if (failed.length) {
                alertBox('danger', failed.length + ' device(s) could not be added:\n' + failureList(failed));
            }
            if (added.length) {
                alertBox('success', 'Added ' + added.length + ' device(s) to rusted.');
            }
        } else {
            alertBox('danger', 'Add failed: ' + errorOf(r));
        }
        loadState();
    });

    document.getElementById('sync-disable').addEventListener('click', async function () {
        const btn = this;
        const devices = selectedIn($right);
        if (!devices.length) return alertBox('warning', 'Select devices from the right list (greyed rows are not selectable).');

        if (!confirm('Disable ' + devices.length + ' device(s) in rusted?\n\n'
            + 'Backups stop, but the devices and their configuration history stay\n'
            + 'in rusted. They reappear on the left and can be re-enabled later.')) return;

        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Disabling&hellip;';
        const r = await apiJson('POST', '/sync/disable', { devices: devices });
        btn.disabled = false;
        btn.innerHTML = '<i class="fa fa-arrow-left"></i> Disable in rusted';

        if (r.ok && r.data) {
            const kept = r.data.kept || [];
            const disabled = r.data.disabled || [];
            kept.forEach(k => errorSet.add(k.name));
            disabled.forEach(n => errorSet.delete(n));
            if (kept.length) {
                alertBox('warning', kept.length + ' device(s) could not be disabled:\n' + failureList(kept));
            }
            if (disabled.length) {
                alertBox('success', 'Disabled ' + disabled.length + ' device(s); their history is preserved.');
            }
        } else {
            alertBox('danger', 'Disable failed: ' + errorOf(r));
        }
        loadState();
    });

    loadDrivers();
    loadCredentials();
    loadState();
})();
</script>
@endsection
