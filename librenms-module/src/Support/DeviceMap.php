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
            if (! is_array($r) || empty($r['name'])) {
                continue;
            }
            $this->byName[strtolower((string) $r['name'])] = $r;
            if (! empty($r['host'])) {
                $this->byHost[strtolower((string) $r['host'])][] = $r;
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
     * The name a LibreNMS device gets in rusted: its display name, or its
     * hostname when the display name is not a valid rusted device name.
     */
    public static function nameFor(Device $d): string
    {
        $name = method_exists($d, 'displayName') ? trim((string) $d->displayName()) : '';

        return self::validName($name) ? $name : (string) $d->hostname;
    }

    /** The rusted device that corresponds to $d, or null if it has none. */
    public function find(Device $d): ?array
    {
        $host = strtolower((string) $d->hostname);

        return $this->byName[strtolower(self::nameFor($d))]
            ?? $this->byName[$host]
            ?? (count($this->byHost[$host] ?? []) === 1 ? $this->byHost[$host][0] : null);
    }

    /** Mirrors rusted's device-name rules (one path segment). */
    private static function validName(string $name): bool
    {
        return $name !== '' && $name !== '.' && $name !== '..'
            && ! str_starts_with($name, '-')
            && ! preg_match('/[\/\\\\\x00-\x1f\x7f]/', $name);
    }
}
