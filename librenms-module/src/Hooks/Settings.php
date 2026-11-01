<?php

namespace AthenaNetworks\RustedLibrenms\Hooks;

use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Foundation\Auth\User;
use LibreNMS\Interfaces\Plugins\Hooks\SettingsHook;

class Settings implements SettingsHook
{
    /** The oldest rusted this plugin works with (device status, upsert edits, transports). */
    public const MIN_RUSTED = '0.2.0';

    public function authorize(User $user): bool
    {
        return $user->can('admin');
    }

    /**
     * @param  array<string, mixed>  $settings
     * @return array<string, mixed>
     */
    public function handle(string $pluginName, array $settings): array
    {
        $version = RustedClient::serverVersion();

        return [
            'content_view' => "$pluginName::settings",
            'settings' => $settings,
            'api_url' => (string) config('rusted-librenms.api_url'),
            'token_set' => config('rusted-librenms.api_token') !== '',
            'healthy' => $version !== null,
            'version' => $version,
            'min_version' => self::MIN_RUSTED,
            'outdated' => $version !== null && self::older($version, self::MIN_RUSTED),
        ];
    }

    /**
     * Whether a reported version is older than $min. Build metadata ("+dirty")
     * is dropped, a prerelease ("-rc.1") counts as older than its release, and
     * a version that isn't semver ("dev-<commit>") is never flagged.
     */
    private static function older(string $version, string $min): bool
    {
        if (preg_match('/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?/', $version, $m) !== 1) {
            return false;
        }

        return version_compare($m[0], $min, '<');
    }
}
