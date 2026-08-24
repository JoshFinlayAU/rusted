@extends('layouts.librenmsv1')

@section('title', 'Rusted Backups')

@section('content')
<div class="container-fluid">
    <h2><i class="fa fa-floppy-o"></i> Rusted &mdash; Configuration Backups
        <a href="{{ route('rusted.sync') }}" class="btn btn-xs btn-default pull-right" title="Bulk device sync">
            <i class="fa fa-exchange"></i> Device sync
        </a>
    </h2>

    <div id="rusted-alerts"></div>

    @csrf

    <div class="panel panel-default">
        <div class="panel-heading">
            <strong>Devices</strong>
            <button id="rusted-refresh" class="btn btn-xs btn-default pull-right" type="button">
                <i class="fa fa-refresh"></i> Refresh
            </button>
        </div>
        <div class="panel-body">
            <table class="table table-condensed table-hover">
                 <thead>
                     <tr>
                         <th>Name</th><th>Host</th><th>Port</th>
                         <th>Driver</th><th>Transport</th><th>Auth</th><th>Group</th>
                         <th>Backup</th><th>Actions</th>
                     </tr>
                 </thead>
             <tbody id="rusted-devices">
                 <tr><td colspan="9"><em>Loading&hellip;</em></td></tr>
             </tbody>
            </table>
        </div>
    </div>

     <div class="panel panel-default">
        <div class="panel-heading"><strong>Add device</strong></div>
        <div class="panel-body">
            <form id="rusted-add" class="form-inline">
                <input class="form-control input-sm" type="text" name="name" placeholder="name" required>
                <input class="form-control input-sm" type="text" name="host" placeholder="host / IP" required>
                <input class="form-control input-sm" type="number" name="port" placeholder="22" style="width:80px">
                <select class="form-control input-sm" name="driver" id="rusted-driver" required>
                    <option value="" disabled selected>driver</option>
                </select>
                <select class="form-control input-sm" name="transport" id="rusted-transport">
                    <option value="" selected>transport (default: ssh)</option>
                </select>
                <select class="form-control input-sm" name="credential" id="rusted-credential" required>
                    <option value="" disabled selected>credential</option>
                </select>
                <input class="form-control input-sm" type="text" name="group" placeholder="group (optional)">
                <button class="btn btn-sm btn-success" type="submit"><i class="fa fa-plus"></i> Add</button>
            </form>
            <p class="help-block" style="margin-top:8px">
                Pick a credential below, or add one with the Credentials panel.
            </p>
        </div>
    </div>

    <div class="panel panel-default">
        <div class="panel-heading"><strong>Credentials</strong></div>
        <div class="panel-body">
            <table class="table table-condensed table-hover">
                <thead>
                    <tr><th>Name</th><th>Username</th><th>Password</th><th>Key</th><th>Enable</th><th>Actions</th></tr>
                </thead>
            <tbody id="rusted-credentials">
                <tr><td colspan="6"><em>Loading&hellip;</em></td></tr>
            </tbody>
            </table>
            <form id="rusted-cred-add" class="form-inline">
                <input class="form-control input-sm" type="text" name="name" placeholder="name" required>
                <input class="form-control input-sm" type="text" name="username" placeholder="username" required autocomplete="off">
                <input class="form-control input-sm" type="password" name="password" placeholder="password" autocomplete="new-password">
                <input class="form-control input-sm" type="password" name="enable" placeholder="enable secret (optional)" autocomplete="new-password">
                <button class="btn btn-sm btn-success" type="submit"><i class="fa fa-plus"></i> Add credential</button>
            </form>
            <p class="help-block" style="margin-top:8px">
                Passwords are sent once to rusted, encrypted at rest, and never returned to the browser.
            </p>
        </div>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
    };
    const DEVICE_VIEW = @json(url('plugin/rusted/device'));

    const $devices = document.getElementById('rusted-devices');
    const $alerts = document.getElementById('rusted-alerts');
    const $driver = document.getElementById('rusted-driver');
    const $transport = document.getElementById('rusted-transport');
    const $form = document.getElementById('rusted-add');
    const $credentials = document.getElementById('rusted-credentials');
    const $credForm = document.getElementById('rusted-cred-add');
    const $credential = document.getElementById('rusted-credential');

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

    async function api(method, path, body) {
        const opts = {
            method: method,
            credentials: 'same-origin',
            headers: {
                'Accept': 'application/json',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': RUSTED.csrf,
            },
        };
        if (body !== undefined) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        const res = await fetch(RUSTED.base + path, opts);
        let data = null;
        try { data = await res.json(); } catch (e) { /* non-JSON */ }
        if (!res.ok) {
            let msg = (data && (data.error || data.message)) || ('HTTP ' + res.status);
            if (data && data.errors) {
                msg = Object.values(data.errors).flat().join(' ');
            }
            throw new Error(msg);
        }
        return data;
    }

    function label(text, kind) {
        return '<span class="label label-' + kind + '">' + esc(text) + '</span>';
    }

    // ---- Backup status indicator ----
    // Shows at a glance whether a device config is current:
    //   never backed up         -> grey circle "never"
    //   < 24h + success/unchanged -> green check "UP TO DATE"
    //   > 24h + success/unchanged -> grey "?" (stale but not failed)
    //   last run failed         -> red triangle with failure message
    function backupStatus(d) {
        const ts = d.last_backup;
        const st = d.last_status;
        if (!ts) {
            return '<span class="text-muted" title="no backups yet"><i class="fa fa-circle-o text-muted"></i> never</span>';
        }
        const dt = new Date(ts);
        const ageH = (Date.now() - dt.getTime()) / 3600000;
        const ok = st === 'success' || st === 'unchanged';
        let icon, color, tip;
        if (st === 'failed') {
            icon = 'fa-exclamation-triangle'; color = 'text-danger';
            tip = 'Failed ' + dt.toLocaleString() + ': ' + (d.last_message || '');
        } else if (!ok) {
            icon = 'fa-question-circle'; color = 'text-muted';
            tip = dt.toLocaleString();
        } else if (ageH < 24) {
            icon = 'fa-check-circle'; color = 'text-success';
            tip = 'Up to date (' + dt.toLocaleString() + ')';
        } else {
            icon = 'fa-question-circle'; color = 'text-muted';
            tip = 'Last backup ' + Math.floor(ageH) + 'h ago (' + dt.toLocaleString() + ')';
        }
        return '<span class="' + color + '" title="' + tip + '" data-toggle="tooltip">' +
            '<i class="fa ' + icon + '"></i>' + '</span>';
    }

    // ---- Transport options ----
    let transportOptions = [];
    async function loadTransports() {
        try {
            transportOptions = await api('GET', '/transports') || [];
        } catch (e) {
            alertBox('warning', 'Could not load transports: ' + e.message);
            transportOptions = ['ssh', 'telnet'];
        }
        // Populate the Add Device form dropdown.
        $transport.innerHTML = '<option value="" selected>transport (default: ssh)</option>' +
            transportOptions.map(t => '<option value="' + esc(t) + '">' + esc(t) + '</option>').join('');
    }

    // ---- Driver options ----
    let driverOptions = [];
    async function loadDrivers() {
        try {
            driverOptions = await api('GET', '/drivers') || [];
            (driverOptions || []).forEach(function (drv) {
                const opt = document.createElement('option');
                opt.value = drv.name;
                opt.textContent = drv.name;
                $driver.appendChild(opt);
            });
        } catch (e) {
            alertBox('warning', 'Could not load drivers: ' + e.message);
        }
    }

    function driverSelect(d) {
        const opts = '<option value="">(auto)</option>' + driverOptions.map(drv =>
            '<option value="' + esc(drv.name) + '" ' + (d.driver === drv.name ? 'selected' : '') + '>' + esc(drv.name) + '</option>'
        ).join('');
        return '<select class="form-control input-sm input-xs rusted-edit" data-field="driver" data-name="' + esc(d.name) + '">' + opts + '</select>';
    }

    function transportSelect(d) {
        const opts = '<option value="" ' + (!d.transport ? 'selected' : '') + '>default (ssh)</option>' +
            transportOptions.map(t => '<option value="' + esc(t) + '" ' + (d.transport === t ? 'selected' : '') + '>' + esc(t) + '</option>'
        ).join('');
        return '<select class="form-control input-sm input-xs rusted-edit" data-field="transport" data-name="' + esc(d.name) + '">' + opts + '</select>';
    }

    // ---- Credential options ----
    let credentialOptions = [];
    function credentialSelect(d) {
        const opts = credentialOptions.map(c =>
            '<option value="' + esc(c.name) + '" ' + (d.credential === c.name ? 'selected' : '') + '>' + esc(c.name) + '</option>'
        ).join('');
        return '<select class="form-control input-sm input-xs rusted-edit" data-field="credential" data-name="' + esc(d.name) + '">' + opts + '</select>';
    }

    function portInput(d) {
        return '<input type="number" class="form-control input-sm input-xs rusted-edit" data-field="port" ' +
            'data-name="' + esc(d.name) + '" value="' + esc(d.port || '') + '" style="width:70px">';
    }

    // ---- Render devices ----
    function renderDevices(devices) {
        if (!devices || !devices.length) {
            $devices.innerHTML = '<tr><td colspan="9"><em>No devices yet.</em></td></tr>';
            return;
        }
        $devices.innerHTML = devices.map(function (d) {
            const n = esc(d.name);
            const isEnabled = (d.enabled !== false);
            const toggleClass = isEnabled ? 'btn-success' : 'btn-danger';
            const toggleTitle = isEnabled
                ? 'Enabled \u2014 click to disable (stops backups, keeps history)'
                : 'Disabled \u2014 click to enable (resumes backups)';
            const authCell = credentialOptions.length
                ? credentialSelect(d)
                : '<span class="text-muted">' + esc(d.credential || '-') + '</span>';
            return '<tr>' +
                '<td>' + esc(d.name) + '</td>' +
                '<td>' + esc(d.host) + '</td>' +
                '<td>' + portInput(d) + '</td>' +
                '<td>' + driverSelect(d) + '</td>' +
                '<td>' + transportSelect(d) + '</td>' +
                '<td>' + authCell + '</td>' +
                '<td>' + (d.group ? esc(d.group) : '-') + '</td>' +
                '<td>' + backupStatus(d) + '</td>' +
                '<td><div class="btn-group btn-group-xs" role="group">' +
                    '<button class="btn btn-xs ' + toggleClass + '" data-action="toggle" data-name="' + n + '" title="' + toggleTitle + '"><i class="fa fa-power-off"></i></button>' +
                    '<button class="btn btn-xs btn-default" data-action="backup" data-name="' + n + '" title="Back up now"><i class="fa fa-download"></i></button>' +
                    '<button class="btn btn-xs btn-default" data-action="history" data-name="' + n + '" title="Show history / view configuration"><i class="fa fa-clock-o"></i></button>' +
                    '<button class="btn btn-xs btn-danger" data-action="remove" data-name="' + n + '" title="Delete (downloads the full config history as a zip first)"><i class="fa fa-trash-o"></i></button>' +
                '</div></td>' +
            '</tr>';
        }).join('');
        // Activate tooltips for backup-status cells.
        const tip = $devices.querySelectorAll('[data-toggle="tooltip"]');
        [].forEach.call(tip, function (el) { $(el).tooltip(); });
    }

    async function loadDevices() {
        try {
            renderDevices(await api('GET', '/devices'));
        } catch (e) {
            $devices.innerHTML = '<tr><td colspan="9"><em>Could not load devices.</em></td></tr>';
            alertBox('warning', e.message);
        }
    }

    function renderCredentials(creds) {
        // Populate the device-form credential dropdown.
        const current = $credential.value;
        $credential.innerHTML = '<option value="" disabled selected>credential</option>' +
            (creds || []).map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + '</option>').join('');
        if (current) $credential.value = current;

        // Populate the credentials table.
        if (!creds || !creds.length) {
            $credentials.innerHTML = '<tr><td colspan="6"><em>No credentials yet.</em></td></tr>';
            return;
        }
        $credentials.innerHTML = creds.map(function (c) {
            const yn = b => b ? label('yes', 'success') : label('no', 'default');
            const n = esc(c.name);
            return '<tr>' +
                '<td>' + esc(c.name) + '</td>' +
                '<td>' + esc(c.username) + '</td>' +
                '<td>' + yn(c.has_password) + '</td>' +
                '<td>' + yn(c.has_key) + '</td>' +
                '<td>' + yn(c.has_enable) + '</td>' +
                '<td><button class="btn btn-xs btn-danger" data-action="remove-cred" data-name="' + n + '">' +
                    '<i class="fa fa-trash"></i> Remove</button></td>' +
            '</tr>';
        }).join('');
    }

    async function loadCredentials() {
        try {
            const creds = await api('GET', '/credentials');
            credentialOptions = creds || [];
            renderCredentials(creds);
            // Re-render devices so auth dropdowns populate.
            loadDevices();
        } catch (e) {
            $credentials.innerHTML = '<tr><td colspan="6"><em>Could not load credentials.</em></td></tr>';
            alertBox('warning', e.message);
        }
    }

    async function doBackup(name, btn) {
        const original = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Backing up&hellip;';
        try {
            const r = await api('POST', '/devices/' + encodeURIComponent(name) + '/backup');
            const status = r.status || 'done';
            const detail = r.message || '';
            const kind = status === 'failed' ? 'danger' : 'success';
            alertBox(kind, 'Backup of ' + name + ': ' + status + (detail ? ' \u2014 ' + detail : ''));
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Backup of ' + name + ' failed: ' + e.message);
        } finally {
            btn.disabled = false;
            btn.innerHTML = original;
        }
    }

    async function downloadBlob(path, filename) {
        const res = await fetch(RUSTED.base + path, {
            method: 'GET',
            credentials: 'same-origin',
            headers: {
                'Accept': 'application/zip',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': RUSTED.csrf,
            },
        });
        if (!res.ok) {
            throw new Error('HTTP ' + res.status);
        }
        const blob = await res.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        setTimeout(() => { URL.revokeObjectURL(url); document.body.removeChild(a); }, 1000);
    }

    // Enable / disable (never delete)
    async function doToggle(name, btn) {
        const isNowEnabled = btn.classList.contains('btn-success');
        try {
            await api('POST', '/devices/' + encodeURIComponent(name) + '/enabled', { enabled: !isNowEnabled });
            alertBox('success', name + ' is now ' + (isNowEnabled ? 'disabled' : 'enabled') + '.');
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Toggle failed: ' + e.message);
        }
    }

    // Remove = a real delete from rusted
    async function doRemove(name) {
        if (!confirm('Download a backup of all configurations and DELETE ' + name +
            ' from rusted?\n\nThis stops backups permanently and removes the device. ' +
            'Its configuration archive will be saved first; the device is only deleted ' +
            'if the download succeeds.')) return;

        const filename = 'rusted-history-' + name + '.zip';
        try {
            await downloadBlob('/devices/' + encodeURIComponent(name) + '/history/zip', filename);
        } catch (e) {
            alertBox('danger', 'Could not download the configuration archive: ' + e.message +
                '\n\nThe device was NOT deleted. Fix this and try again, or use the rusted CLI.');
            return;
        }

        try {
            await api('DELETE', '/devices/' + encodeURIComponent(name));
            alertBox('success', name + ' deleted from rusted (configuration archive saved).');
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Delete failed: ' + e.message +
                '\n\nThe archive was saved, but the device could not be removed.');
        }
    }

    // Inline edit: save a single field change immediately.
    let editTimeouts = {};
    async function doInlineEdit(name, field, value) {
        try {
            await api('PUT', '/devices/' + encodeURIComponent(name), { [field]: value });
        } catch (e) {
            alertBox('danger', 'Could not update ' + field + ' for ' + name + ': ' + e.message);
        }
    }

    // Event delegation for per-row action buttons.
    $devices.addEventListener('click', function (e) {
        const btn = e.target.closest('button[data-action]');
        if (!btn) return;
        const name = btn.getAttribute('data-name');
        const action = btn.dataset.action;
        if (action === 'toggle') doToggle(name, btn);
        else if (action === 'backup') doBackup(name, btn);
        else if (action === 'history') window.location.href = DEVICE_VIEW + '/' + encodeURIComponent(name);
        else if (action === 'remove') doRemove(name);
    });

     // Event delegation for inline-edit inputs/selections inside device rows.
    $devices.addEventListener('change', function (e) {
        const el = e.target.closest('.rusted-edit');
        if (!el) return;
        const name = el.getAttribute('data-name');
        const field = el.getAttribute('data-field');
        let value;
        if (el.tagName === 'SELECT') {
            value = el.value;
        } else if (el.type === 'number') {
            value = el.value === '' ? null : parseInt(el.value, 10);
        } else {
            value = el.value;
        }
        // Clear any pending debounce.
        if (editTimeouts[name + field]) clearTimeout(editTimeouts[name + field]);
        // Immediate save for selects; debounced save for text inputs.
        if (el.tagName === 'SELECT') {
            doInlineEdit(name, field, value);
        } else {
            editTimeouts[name + field] = setTimeout(() => doInlineEdit(name, field, value), 500);
        }
    });

    // Event delegation for credential remove buttons.
    $credentials.addEventListener('click', function (e) {
        const btn = e.target.closest('button[data-action="remove-cred"]');
        if (!btn) return;
        doRemoveCredential(btn.getAttribute('data-name'));
    });

    async function doRemoveCredential(name) {
        if (!confirm('Remove credential ' + name + '?')) return;
        try {
            await api('DELETE', '/credentials/' + encodeURIComponent(name));
            alertBox('success', 'Credential ' + name + ' removed.');
            loadCredentials();
        } catch (e) {
            alertBox('danger', 'Remove failed: ' + e.message);
        }
    }

    $credForm.addEventListener('submit', async function (e) {
        e.preventDefault();
        const fd = new FormData($credForm);
        const payload = {
            name: fd.get('name'),
            username: fd.get('username'),
            password: fd.get('password') || '',
            enable: fd.get('enable') || '',
        };
        try {
            await api('POST', '/credentials', payload);
            alertBox('success', 'Credential ' + payload.name + ' added.');
            $credForm.reset();
            loadCredentials();
        } catch (e) {
            alertBox('danger', 'Add failed: ' + e.message);
        }
    });

    document.getElementById('rusted-refresh').addEventListener('click', function () {
        loadCredentials();
        loadDrivers();
        loadTransports();
    });

    $form.addEventListener('submit', async function (e) {
        e.preventDefault();
        const fd = new FormData($form);
        const payload = {
            name: fd.get('name'),
            host: fd.get('host'),
            driver: fd.get('driver'),
            credential: fd.get('credential'),
            group: fd.get('group') || '',
        };
        const port = fd.get('port');
        if (port) payload.port = parseInt(port, 10);
        const transport = fd.get('transport');
        if (transport) payload.transport = transport;
        try {
            await api('POST', '/devices', payload);
            alertBox('success', 'Device ' + payload.name + ' added.');
            $form.reset();
            $transport.selectedIndex = 0;
            loadDevices();
        } catch (e) {
            alertBox('danger', 'Add failed: ' + e.message);
        }
    });

    // Initial load. Credentials first (so auth dropdowns populate),
    // then devices, drivers, transports.
    loadCredentials();
    loadDrivers();
    loadTransports();
})();
</script>
@endsection
