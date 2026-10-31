<?php

namespace AthenaNetworks\RustedLibrenms\Hooks;

use LibreNMS\Interfaces\Plugins\Hooks\MenuEntryHook;

class MenuEntry implements MenuEntryHook
{
    /** Only LibreNMS administrators can use the plugin (see routes/web.php). */
    public function authorize(): bool
    {
        return (bool) auth()->user()?->can('admin');
    }

    /**
     * @return array{0: string, 1: array<string, mixed>}
     */
    public function handle(string $pluginName): array
    {
        return ["$pluginName::menu", []];
    }
}
