<?php

namespace AthenaNetworks\RustedLibrenms\Controllers;

use App\Models\Device;
use AthenaNetworks\RustedLibrenms\Support\DeviceMap;
use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Contracts\View\View;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Routing\Controller;
use Illuminate\Support\Facades\Gate;
use Symfony\Component\HttpFoundation\Response as SymfonyResponse;
use ZipArchive;

class RustedController extends Controller
{
    /**
     * Fields of a rusted device record that are status, not configuration.
     * They are stripped before a record is POSTed back for an upsert.
     */
    private const READ_ONLY_FIELDS = ['last_backup', 'last_status', 'last_message'];

    /**
     * Every action in this controller requires LibreNMS's `admin` gate. The
     * routes carry `can:admin` too; checking here as well means a route added
     * without the middleware still cannot leak configs or change devices.
     *
     * @param  string  $method
     * @param  array<int|string, mixed>  $parameters
     */
    public function callAction($method, $parameters): mixed
    {
        abort_unless(Gate::allows('admin'), 403, 'Rusted is restricted to LibreNMS administrators.');

        return parent::callAction($method, $parameters);
    }

    /** Render the page shell; all data is loaded by the browser via AJAX. */
    public function index(): View
    {
        return view('rusted::page', ['title' => 'Rusted Backups']);
    }

    /** Render the per-device page (config viewer, revisions, diff, runs). */
    public function device(string $name): View
    {
        return view('rusted::device', ['hostname' => $name]);
    }

    /** Render the bulk-sync page shell. */
    public function sync(): View
    {
        return view('rusted::sync');
    }

    // --- AJAX/JSON receiver -------------------------------------------------
    // These are same-origin routes the browser calls. Each relays to the rusted
    // API server-side and forwards rusted's JSON body and status code.

    public function apiDevices(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/devices'));
    }

    public function apiDrivers(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/drivers'));
    }

    /**
     * Transport names. An older rusted without GET /api/transports answers
     * 404; that is reported as an empty list so the UI simply offers the
     * server's default transport instead of showing an error.
     */
    public function apiTransports(): JsonResponse
    {
        try {
            $resp = RustedClient::client()->get('/api/transports');
        } catch (\Throwable $e) {
            return $this->unreachable($e);
        }
        if ($resp->status() === 404) {
            return response()->json([]);
        }

        return $this->relay($resp);
    }

    public function apiCredentials(): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get('/api/credentials'));
    }

    public function apiAddCredential(Request $request): JsonResponse
    {
        $data = $request->validate([
            'name' => 'required|string',
            'username' => 'required|string',
            'password' => 'nullable|string',
            'enable' => 'nullable|string',
        ]);

        return $this->proxy(fn () => RustedClient::client()->post('/api/credentials', $data));
    }

    public function apiRemoveCredential(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->delete('/api/credentials/'.rawurlencode($name)));
    }

    public function apiDevice(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get($this->devicePath($name)));
    }

    public function apiAddDevice(Request $request): JsonResponse
    {
        $data = $request->validate([
            'name' => 'required|string',
            'host' => 'required|string',
            'port' => 'nullable|integer',
            'driver' => 'required|string',
            'credential' => 'required|string',
            'group' => 'nullable|string',
            'transport' => 'nullable|string',
        ]);

        // Go's json.Decoder rejects a JSON string for an int field, and empty
        // optional fields arrive as null; send rusted only what was given.
        $data = array_filter($data, fn ($v) => $v !== null);
        if (isset($data['port'])) {
            $data['port'] = (int) $data['port'];
        }

        return $this->proxy(fn () => RustedClient::client()->post('/api/devices', $data));
    }

    public function apiRemoveDevice(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->delete($this->devicePath($name)));
    }

    /**
     * Enable or disable a device. Disabling stops backups but keeps the device
     * and its history. Body: {"enabled": true|false}
     */
    public function apiSetEnabled(string $name, Request $request): JsonResponse
    {
        $enabled = $request->validate(['enabled' => 'required|boolean'])['enabled'];

        return $this->upsertDevice($name, ['enabled' => (bool) $enabled]);
    }

    /**
     * Partially update a device. Only the keys present in the request body
     * are changed; an empty value clears an optional field (e.g. group, or
     * port/transport back to rusted's default). Everything else, including
     * fields this plugin does not know about, is passed through unchanged.
     *
     * Body: any subset of {"driver","port","transport","group","credential"}
     */
    public function apiUpdateDevice(string $name, Request $request): JsonResponse
    {
        $request->validate([
            'driver' => 'nullable|string',
            'port' => 'nullable|integer|min:0|max:65535',
            'transport' => 'nullable|string',
            'group' => 'nullable|string',
            'credential' => 'nullable|string',
        ]);

        $changes = [];
        foreach (['driver', 'transport', 'group', 'credential'] as $field) {
            // has() (key present) rather than ??: Laravel converts "" to null, and a
            // cleared field must be sent as "", not fall back to the old value.
            if ($request->has($field)) {
                $changes[$field] = (string) $request->input($field, '');
            }
        }
        if ($request->has('port')) {
            $changes['port'] = (int) $request->input('port', 0); // 0 = rusted default
        }
        if (isset($changes['credential']) && $changes['credential'] === '') {
            return response()->json(['error' => 'A device must have a credential.'], 422);
        }
        if ($changes === []) {
            return response()->json(['error' => 'Nothing to update.'], 422);
        }

        return $this->upsertDevice($name, $changes);
    }

    public function apiBackup(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->post($this->devicePath($name).'/backup'));
    }

    public function apiHistory(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get($this->devicePath($name).'/history'));
    }

    /**
     * A device's stored configuration, optionally at ?commit=<hash>. Relayed
     * as raw text/plain (never JSON-encoded).
     */
    public function apiConfig(string $name, Request $request): SymfonyResponse
    {
        $query = array_filter(['commit' => (string) $request->query('commit', '')]);

        return $this->proxyText(fn () => RustedClient::client()->get($this->devicePath($name).'/config', $query));
    }

    /** List the git revisions of a device's config (newest first). JSON. */
    public function apiVersions(string $name): JsonResponse
    {
        return $this->proxy(fn () => RustedClient::client()->get($this->devicePath($name).'/versions'));
    }

    /**
     * Unified diff of a device's config: ?from=<a> alone shows what that
     * revision changed; ?from=<a>&to=<b> diffs two. Relayed as text/plain.
     */
    public function apiDiff(string $name, Request $request): SymfonyResponse
    {
        $query = array_filter([
            'from' => (string) $request->query('from', ''),
            'to' => (string) $request->query('to', ''),
        ]);

        return $this->proxyText(fn () => RustedClient::client()->get($this->devicePath($name).'/diff', $query));
    }

    /**
     * Download a device's full configuration history (every stored revision
     * plus the latest copy) as a zip, with a MANIFEST.txt describing each
     * revision. Used to archive a device before it is deleted.
     */
    public function apiHistoryZip(string $name): SymfonyResponse
    {
        if (! class_exists(ZipArchive::class)) {
            return $this->text('The PHP zip extension is not installed on the LibreNMS server.', 501);
        }

        $safe = $this->safeName($name);
        $path = tempnam(sys_get_temp_dir(), 'rusted-history-');
        $zip = new ZipArchive();
        if ($path === false || $zip->open($path, ZipArchive::CREATE | ZipArchive::OVERWRITE) !== true) {
            if ($path !== false) {
                @unlink($path);
            }

            return $this->text('Could not create the archive.', 500);
        }

        $got = $this->snapshotDevice($zip, $name, $safe);
        $zip->close();

        if ($got <= 0) {
            @unlink($path);

            return $this->text('No configuration history for this device.', 404);
        }

        return response()
            ->download($path, 'rusted-history-'.$safe.'.zip', ['Content-Type' => 'application/zip'])
            ->deleteFileAfterSend(true);
    }

    // --- Bulk sync -----------------------------------------------------------

    /**
     * Join LibreNMS devices against the rusted inventory (see DeviceMap for how
     * they are matched):
     *  - left:  LibreNMS devices not actively backed up (absent from rusted,
     *           or disabled there; `rust_state` tells which).
     *  - right: enabled rusted devices that are also in LibreNMS.
     *  - grey:  rusted devices that are not in LibreNMS (shown locked).
     */
    public function syncState(): JsonResponse
    {
        try {
            $resp = RustedClient::client()->get('/api/devices');
        } catch (\Throwable $e) {
            return $this->unreachable($e);
        }
        if (! $resp->successful()) {
            return $this->relay($resp);
        }
        $rusted = is_array($resp->json()) ? $resp->json() : [];
        $map = new DeviceMap($rusted);

        $libre = Device::query()
            ->whereNotNull('hostname')
            ->where('hostname', '!=', '')
            ->orderBy('hostname')
            ->get();

        // Left: LibreNMS devices with no enabled rusted device, listed under the
        // name they have (or would get) in rusted.
        $left = [];
        $matched = [];
        foreach ($libre as $d) {
            $r = $map->find($d);
            if ($r !== null) {
                $matched[strtolower((string) $r['name'])] = true;
                if ((bool) ($r['enabled'] ?? true)) {
                    continue;
                }
            }
            $left[] = [
                'id' => $d->device_id,
                'name' => $r['name'] ?? DeviceMap::nameFor($d),
                'hostname' => $d->hostname,
                'os' => $d->os,
                'status' => (bool) $d->status,
                'disabled' => (bool) $d->disabled,
                'rust_state' => $r !== null ? 'disabled' : 'absent',
            ];
        }

        // Right: enabled rusted devices; greyed when no LibreNMS device maps to them.
        $right = [];
        $grey = [];
        foreach ($rusted as $r) {
            if (! is_array($r) || empty($r['name'])) {
                continue;
            }
            $entry = [
                'name' => $r['name'],
                'host' => $r['host'] ?? '',
                'driver' => $r['driver'] ?? '',
                'group' => $r['group'] ?? '',
                'enabled' => (bool) ($r['enabled'] ?? true),
            ];
            if (! isset($matched[strtolower((string) $r['name'])])) {
                $grey[] = $entry;
            } elseif ($entry['enabled']) {
                $right[] = $entry;
            }
        }

        return response()->json(['left' => $left, 'right' => $right, 'grey' => $grey]);
    }

    /**
     * Add LibreNMS devices (by device_id) to rusted in one batch with a single
     * driver and credential. A device that already has a rusted device (see
     * DeviceMap) updates and re-enables that one; any other is created under
     * its LibreNMS display name, connecting to its LibreNMS hostname.
     */
    public function syncAdd(Request $request): JsonResponse
    {
        $data = $request->validate([
            'driver' => 'required|string',
            'credential' => 'required|string',
            'devices' => 'required|array|min:1',
            'devices.*' => 'required|integer',
        ]);

        try {
            $map = DeviceMap::load();
        } catch (\Throwable $e) {
            return $this->unreachable($e);
        }
        $libre = Device::query()->whereIn('device_id', $data['devices'])->get()->keyBy('device_id');

        $added = [];
        $failed = [];
        foreach ($data['devices'] as $id) {
            $d = $libre->get($id);
            if ($d === null) {
                $failed[] = ['name' => (string) $id, 'error' => 'not a LibreNMS device'];
                continue;
            }
            $name = $map->find($d)['name'] ?? DeviceMap::nameFor($d);

            $changes = ['driver' => $data['driver'], 'credential' => $data['credential'], 'enabled' => true];
            $r = $this->upsertDevice($name, $changes, ['name' => $name, 'host' => $d->hostname]);
            if ($r->getStatusCode() < 300) {
                $added[] = $name;
            } else {
                $failed[] = ['name' => $name, 'error' => $r->getData(true)['error'] ?? ('HTTP '.$r->getStatusCode())];
            }
        }

        return response()->json(['added' => $added, 'failed' => $failed]);
    }

    /**
     * The rusted side of one LibreNMS device, for its Overview panel: the name
     * of its rusted device (null if it has none), plus the name and host it
     * would be added with.
     */
    public function libreDevice(int $id): JsonResponse
    {
        $d = Device::find($id);
        if ($d === null) {
            return response()->json(['error' => 'not a LibreNMS device'], 404);
        }
        try {
            $map = DeviceMap::load();
        } catch (\Throwable $e) {
            return $this->unreachable($e);
        }

        return response()->json([
            'name' => $map->find($d)['name'] ?? null,
            'suggested' => DeviceMap::nameFor($d),
            'host' => $d->hostname,
        ]);
    }

    /**
     * Disable (never delete) devices in rusted, so their configuration history
     * is preserved. Devices that fail are returned in `kept`.
     */
    public function syncDisable(Request $request): JsonResponse
    {
        $data = $request->validate([
            'devices' => 'required|array|min:1',
            'devices.*' => 'required|string',
        ]);

        $disabled = [];
        $kept = [];
        foreach ($data['devices'] as $host) {
            $r = $this->upsertDevice($host, ['enabled' => false]);
            if ($r->getStatusCode() < 300) {
                $disabled[] = $host;
            } else {
                $kept[] = ['name' => $host, 'error' => $r->getData(true)['error'] ?? ('HTTP '.$r->getStatusCode())];
            }
        }

        return response()->json(['disabled' => $disabled, 'kept' => $kept]);
    }

    // --- Helpers -------------------------------------------------------------

    /**
     * Change some fields of a device through rusted's upsert (POST
     * /api/devices). The current record is fetched and POSTed back with
     * $changes applied, so every field not being edited, including ones this
     * plugin does not know about (e.g. cmd_timeout / idle_timeout), keeps its
     * value. With $create set, a missing device is created from $create +
     * $changes instead of failing.
     *
     * @param  array<string, mixed>  $changes
     * @param  array<string, mixed>|null  $create
     */
    private function upsertDevice(string $name, array $changes, ?array $create = null): JsonResponse
    {
        try {
            $g = RustedClient::client()->get($this->devicePath($name));
        } catch (\Throwable $e) {
            return $this->unreachable($e);
        }

        if ($g->successful() && is_array($g->json())) {
            $record = array_diff_key($g->json(), array_flip(self::READ_ONLY_FIELDS));
        } elseif ($g->status() === 404 && $create !== null) {
            $record = $create;
        } else {
            return $this->relay($g);
        }

        $payload = array_merge($record, $changes);

        return $this->proxy(fn () => RustedClient::client()->post('/api/devices', $payload));
    }

    /**
     * Write every stored revision (and the latest copy) of a device into $zip
     * under "<safe>/", plus a MANIFEST.txt. Returns the number of configs
     * written (0 = nothing to export).
     */
    private function snapshotDevice(ZipArchive $zip, string $name, string $safe): int
    {
        try {
            $v = RustedClient::client()->get($this->devicePath($name).'/versions');
            $versions = $v->successful() && is_array($v->json()) ? $v->json() : [];
        } catch (\Throwable) {
            $versions = [];
        }

        $got = 0;
        $hasLatest = false;
        try {
            $latest = RustedClient::client()->get($this->devicePath($name).'/config');
            if ($latest->successful() && $latest->body() !== '') {
                $zip->addFromString($safe.'/latest.cfg', $latest->body());
                $hasLatest = true;
                $got++;
            }
        } catch (\Throwable) {
            // no backup yet
        }

        $seen = [];
        foreach ($versions as $ver) {
            $commit = is_array($ver) ? (string) ($ver['commit'] ?? '') : '';
            // Hex only: the hash becomes a file name inside the archive.
            if (! preg_match('/^[0-9a-f]{4,64}$/i', $commit) || isset($seen[$commit])) {
                continue;
            }
            try {
                $c = RustedClient::client()->get($this->devicePath($name).'/config', ['commit' => $commit]);
                if ($c->successful()) {
                    $zip->addFromString($safe.'/'.$commit.'.cfg', $c->body());
                    $seen[$commit] = true;
                    $got++;
                }
            } catch (\Throwable) {
                continue;
            }
        }

        if ($got > 0) {
            $zip->addFromString($safe.'/MANIFEST.txt', $this->buildManifest($name, $versions, $hasLatest));
        }

        return $got;
    }

    /**
     * Human-readable index of the revisions in a device archive.
     *
     * @param  array<int, mixed>  $versions  rusted /versions entries
     */
    private function buildManifest(string $name, array $versions, bool $hasLatest): string
    {
        $out = "rusted configuration history\n"
            ."============================\n"
            .'device     : '.$name."\n"
            .'generated  : '.date('c')."\n"
            .'revisions  : '.count($versions)."\n"
            ."\nFiles\n-----\n"
            ."  <commit>.cfg  one stored revision of the device configuration\n";
        if ($hasLatest) {
            $out .= "  latest.cfg    the most recent on-disk copy at export time\n";
        }
        $out .= "\nRevisions (newest first)\n------------------------\n";

        if (! $versions) {
            return $out."(no git revisions recorded)\n";
        }

        $out .= str_pad('commit', 9).str_pad('date', 27)."subject\n";
        foreach ($versions as $ver) {
            if (! is_array($ver)) {
                continue;
            }
            $when = (string) ($ver['captured_at'] ?? $ver['date'] ?? '-');
            $out .= str_pad(substr((string) ($ver['commit'] ?? ''), 0, 8), 9)
                .str_pad(substr($when, 0, 26), 27)
                .str_replace(["\r", "\n"], ' ', (string) ($ver['subject'] ?? ''))."\n";
        }

        return $out;
    }

    /** rusted API path of a device. */
    private function devicePath(string $name): string
    {
        return '/api/devices/'.rawurlencode($name);
    }

    /**
     * A device name reduced to characters safe in a file name, a zip entry
     * path and a Content-Disposition header (no "/", "..", quotes or CR/LF).
     */
    private function safeName(string $name): string
    {
        $safe = trim((string) preg_replace('/[^A-Za-z0-9._-]+/', '_', $name), '._');

        return $safe === '' ? 'device' : substr($safe, 0, 128);
    }

    private function text(string $body, int $status): SymfonyResponse
    {
        return response($body, $status)->header('Content-Type', 'text/plain; charset=utf-8');
    }

    private function unreachable(\Throwable $e): JsonResponse
    {
        return response()->json(['error' => 'Could not reach the rusted API: '.$e->getMessage()], 502);
    }

    /**
     * Execute a rusted API call that returns text and forward its raw body and
     * status. Transport failures become a clean 502 text response.
     *
     * @param  callable(): \Illuminate\Http\Client\Response  $call
     */
    private function proxyText(callable $call): SymfonyResponse
    {
        try {
            $resp = $call();
        } catch (\Throwable $e) {
            return $this->text('Could not reach the rusted API: '.$e->getMessage(), 502);
        }

        // Errors from rusted are JSON; pass them on as such so the UI can show
        // the message. Successful bodies are always served as plain text.
        if (! $resp->successful()) {
            return $this->relay($resp);
        }

        return $this->text($resp->body(), $resp->status());
    }

    /**
     * Execute a rusted API call and forward its JSON + status, turning any
     * transport failure into a clean 502 so the browser always gets JSON.
     *
     * @param  callable(): \Illuminate\Http\Client\Response  $call
     */
    private function proxy(callable $call): JsonResponse
    {
        try {
            $resp = $call();
        } catch (\Throwable $e) {
            return $this->unreachable($e);
        }

        return $this->relay($resp);
    }

    private function relay(\Illuminate\Http\Client\Response $resp): JsonResponse
    {
        $body = $resp->json();
        if (! is_array($body)) {
            $body = ['raw' => $resp->body()];
        }

        return response()->json($body, $resp->status() ?: 502);
    }
}
