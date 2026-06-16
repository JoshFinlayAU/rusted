<div class="panel panel-default panel-condensed">
    <div class="panel-heading">
        <i class="fa fa-floppy-o"></i> <strong>Configuration Backups</strong>
        <a class="pull-right" href="{{ route('rusted.index') }}" title="Open Rusted">
            <i class="fa fa-external-link"></i>
        </a>
    </div>
    <div class="panel-body" id="rusted-do-body">
        <em>Loading&hellip;</em>
    </div>
</div>

<script>
(function () {
    const RUSTED = {
        base: @json(url('plugin/rusted/api')),
        csrf: @json(csrf_token()),
        host: @json($hostname),
    };
    const $body = document.getElementById('rusted-do-body');

    function esc(s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        }[c]));
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
        try { data = await res.json(); } catch (e) { /* */ }
        return { ok: res.ok, status: res.status, data: data };
    }

    function label(text, kind) {
        return '<span class="label label-' + kind + '">' + esc(text) + '</span>';
    }

    function statusKind(s) {
        return s === 'success' ? 'success' : (s === 'failed' ? 'danger' : (s === 'unchanged' ? 'info' : 'default'));
    }

    const dev = encodeURIComponent(RUSTED.host);

    async function render() {
        $body.innerHTML = '<em>Loading&hellip;</em>';
        const d = await api('GET', '/devices/' + dev);

        if (d.status === 404) {
            return renderUnmanaged();
        }
        if (!d.ok) {
            $body.innerHTML = '<div class="text-warning">Rusted API error: ' +
                esc((d.data && d.data.error) || ('HTTP ' + d.status)) + '</div>';
            return;
        }
        renderManaged(d.data);
    }

    async function renderManaged(device) {
        const h = await api('GET', '/devices/' + dev + '/history');
        const runs = (h.ok && Array.isArray(h.data)) ? h.data : [];
        const last = runs[0];

        let lastHtml = '<em>no backups yet</em>';
        if (last) {
            lastHtml = label(last.status, statusKind(last.status)) +
                ' <span class="text-muted">' + esc(last.started_at) + '</span>' +
                (last.commit ? ' <code>' + esc(String(last.commit).substring(0, 8)) + '</code>' : '');
        }

        $body.innerHTML =
            '<table class="table table-condensed" style="margin-bottom:8px">' +
            '<tr><th style="width:120px">Driver</th><td>' + esc(device.driver) + '</td></tr>' +
            '<tr><th>Last backup</th><td>' + lastHtml + '</td></tr>' +
            '</table>' +
            '<button class="btn btn-xs btn-primary" id="rusted-do-backup"><i class="fa fa-download"></i> Back up now</button>';

        document.getElementById('rusted-do-backup').addEventListener('click', doBackup);
    }

    async function doBackup(e) {
        const btn = e.currentTarget;
        const original = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = '<i class="fa fa-spinner fa-spin"></i> Backing up&hellip;';
        const r = await api('POST', '/devices/' + dev + '/backup');
        btn.disabled = false;
        btn.innerHTML = original;
        if (!r.ok) {
            alert('Backup failed: ' + ((r.data && r.data.error) || ('HTTP ' + r.status)));
        }
        render();
    }

    async function renderUnmanaged() {
        const drv = await api('GET', '/drivers');
        const cred = await api('GET', '/credentials');
        const drivers = (drv.ok && Array.isArray(drv.data)) ? drv.data : [];
        const creds = (cred.ok && Array.isArray(cred.data)) ? cred.data : [];

        const driverOpts = drivers.map(d => '<option value="' + esc(d.name) + '">' + esc(d.name) + '</option>').join('');
        const credOpts = creds.map(c => '<option value="' + esc(c.name) + '">' + esc(c.name) + ' (' + esc(c.username) + ')</option>').join('');

        $body.innerHTML =
            '<p class="text-muted">This device is not managed by rusted.</p>' +
            '<div class="form-group"><select id="rusted-do-driver" class="form-control input-sm">' +
                '<option value="" disabled selected>driver</option>' + driverOpts + '</select></div>' +
            '<div class="form-group"><select id="rusted-do-cred" class="form-control input-sm">' +
                (creds.length ? credOpts : '<option value="" disabled selected>no credentials — add one in Rusted</option>') +
            '</select></div>' +
            '<button class="btn btn-xs btn-success" id="rusted-do-add" ' + (creds.length ? '' : 'disabled') + '>' +
                '<i class="fa fa-plus"></i> Add to rusted</button>';

        const addBtn = document.getElementById('rusted-do-add');
        if (addBtn) addBtn.addEventListener('click', doAdd);
    }

    async function doAdd() {
        const driver = document.getElementById('rusted-do-driver').value;
        const credential = document.getElementById('rusted-do-cred').value;
        if (!driver || !credential) {
            alert('Pick a driver and credential.');
            return;
        }
        const r = await api('POST', '/devices', {
            name: RUSTED.host, host: RUSTED.host, driver: driver, credential: credential,
        });
        if (!r.ok) {
            alert('Add failed: ' + ((r.data && (r.data.error || r.data.message)) || ('HTTP ' + r.status)));
            return;
        }
        render();
    }

    render();
})();
</script>
