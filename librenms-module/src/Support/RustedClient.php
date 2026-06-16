<?php

namespace AthenaNetworks\RustedLibrenms\Support;

use Illuminate\Http\Client\PendingRequest;
use Illuminate\Support\Facades\Http;

/**
 * Thin wrapper around the rusted HTTP API. The bearer token never leaves the
 * LibreNMS server — the browser only ever talks to LibreNMS, which proxies to
 * rusted on the user's behalf.
 */
class RustedClient
{
    public static function client(): PendingRequest
    {
        return Http::baseUrl(rtrim((string) config('rusted-librenms.api_url'), '/'))
            ->withToken((string) config('rusted-librenms.api_token'))
            ->acceptJson()
            ->timeout((int) config('rusted-librenms.timeout', 120));
    }

    /** @return array<int, array<string, mixed>> */
    public static function devices(): array
    {
        return self::client()->get('/api/devices')->json() ?? [];
    }

    /** @return array<int, array<string, mixed>> */
    public static function drivers(): array
    {
        return self::client()->get('/api/drivers')->json() ?? [];
    }

    /** @return array<int, array<string, mixed>> */
    public static function history(string $name): array
    {
        return self::client()->get("/api/devices/{$name}/history")->json() ?? [];
    }

    /** @param array<string, mixed> $payload */
    public static function addDevice(array $payload): \Illuminate\Http\Client\Response
    {
        return self::client()->post('/api/devices', $payload);
    }

    public static function removeDevice(string $name): \Illuminate\Http\Client\Response
    {
        return self::client()->delete("/api/devices/{$name}");
    }

    public static function backup(string $name): \Illuminate\Http\Client\Response
    {
        return self::client()->post("/api/devices/{$name}/backup");
    }

    /** Quick reachability probe for the settings page. */
    public static function healthy(): bool
    {
        try {
            return self::client()->get('/healthz')->successful();
        } catch (\Throwable) {
            return false;
        }
    }
}
