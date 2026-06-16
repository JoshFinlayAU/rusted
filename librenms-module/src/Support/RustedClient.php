<?php

namespace AthenaNetworks\RustedLibrenms\Support;

use Illuminate\Http\Client\PendingRequest;
use Illuminate\Support\Facades\Http;

/**
 * Thin builder for talking to the rusted HTTP API from the LibreNMS server.
 *
 * The bearer token never leaves the LibreNMS host: the browser only ever calls
 * this plugin's own same-origin AJAX routes, which use this client to relay to
 * rusted server-side. rusted therefore only needs to be reachable from the
 * LibreNMS server (e.g. 127.0.0.1:8080) — no reverse proxy or public exposure.
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
