<?php

namespace AthenaNetworks\RustedLibrenms\Support;

use App\Models\Device;

/**
 * Maps LibreNMS devices to rusted devices and back.
 *
 * A device added from LibreNMS is named after its LibreNMS display name, so
 * LibreNMS's "show sysName instead of hostname" setting (device_display_default,
 * or a device's own display name) carries over to rusted, the backup repo and
 * this plugin. rusted still connects to the LibreNMS hostname/IP.
 *
 * An existing rusted device matches a LibreNMS device when its name is the
 * display name, else when its name is the hostname, else when its host is the
 * hostname (and no other rusted device has that host). The last two keep
 * devices that were added by IP, or named by hand, attached to their
 * LibreNMS device.
 */
class DeviceMap
{
    /** @var array<string, array<string, mixed>> rusted devices by lowercased name */
    private array $byName = [];

    /** @var array<string, list<array<string, mixed>>> rusted devices by lowercased host */
    private array $byHost = [];

    /** @param  array<int, mixed>  $rusted  rows from GET /api/devices */
    public function __construct(array $rusted)
    {
        foreach ($rusted as $r) {
            if (is_array($r)) {
                $this->add($r);
            }
        }
    }

    /** Builds a map from rusted's current device list. */
    public static function load(): self
    {
        $resp = RustedClient::client()->get('/api/devices');
        $resp->throw();

        return new self(is_array($resp->json()) ? $resp->json() : []);
    }

    /**
     * Records a rusted device (e.g. one just added), so later lookups and
     * nameFor() calls in the same batch take it into account.
     *
     * @param  array<string, mixed>  $r
     */
    public function add(array $r): void
    {
        $name = (string) ($r['name'] ?? '');
        if ($name === '') {
            return;
        }
        $this->byName[strtolower($name)] = $r;
        $host = (string) ($r['host'] ?? '');
        if ($host !== '') {
            $this->byHost[strtolower($host)][] = $r;
        }
    }

    /** The rusted device that corresponds to $d, or null if it has none. */
    public function find(Device $d): ?array
    {
        $host = strtolower((string) $d->hostname);

        // A display-name match only counts on the same host: two LibreNMS
        // devices can share a sysName, and must not share a rusted device.
        $display = self::displayName($d);
        $r = $display !== null ? ($this->byName[strtolower($display)] ?? null) : null;
        if ($r !== null && strtolower((string) ($r['host'] ?? '')) === $host) {
            return $r;
        }

        return $this->byName[$host]
            ?? (count($this->byHost[$host] ?? []) === 1 ? $this->byHost[$host][0] : null);
    }

    /**
     * The name a LibreNMS device gets when added to rusted: its display name,
     * or its hostname when the display name is not a valid rusted device name
     * or already belongs to a rusted device on another host.
     */
    public function nameFor(Device $d): string
    {
        $display = self::displayName($d);
        if ($display !== null) {
            $taken = $this->byName[strtolower($display)] ?? null;
            if ($taken === null || strtolower((string) ($taken['host'] ?? '')) === strtolower((string) $d->hostname)) {
                return $display;
            }
        }

        return (string) $d->hostname;
    }

    /** $d's LibreNMS display name, or null if rusted can't use it as a device name. */
    private static function displayName(Device $d): ?string
    {
        $name = method_exists($d, 'displayName') ? trim((string) $d->displayName()) : '';

        return self::validName($name) ? $name : null;
    }

    /**
     * Mirrors rusted's device-name rules (one path segment, no control
     * characters: \p{Cc} covers C0, DEL and C1 like Go's unicode.IsControl).
     * Invalid UTF-8 makes preg_match fail, which also rejects the name.
     */
    private static function validName(string $name): bool
    {
        return $name !== '' && $name !== '.' && $name !== '..'
            && ! str_starts_with($name, '-')
            && preg_match('/[\/\\\\\p{Cc}]/u', $name) === 0;
    }
}
