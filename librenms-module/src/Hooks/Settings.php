<?php

namespace AthenaNetworks\RustedLibrenms\Hooks;

use AthenaNetworks\RustedLibrenms\Support\RustedClient;
use Illuminate\Foundation\Auth\User;
use LibreNMS\Interfaces\Plugins\Hooks\SettingsHook;

class Settings implements SettingsHook
{
    public function authorize(User $user): bool
    {
        return true;
    }

    /**
     * @param  array<string, mixed>  $settings
     * @return array<string, mixed>
     */
    public function handle(string $pluginName, array $settings): array
    {
        return [
            'content_view' => "$pluginName::settings",
            'settings' => $settings,
            'api_url' => (string) config('rusted-librenms.api_url'),
            'token_set' => config('rusted-librenms.api_token') !== '',
            'healthy' => RustedClient::healthy(),
        ];
    }
}
