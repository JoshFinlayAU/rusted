<?php

namespace AthenaNetworks\RustedLibrenms\Hooks;

use LibreNMS\Interfaces\Plugins\Hooks\MenuEntryHook;

class MenuEntry implements MenuEntryHook
{
    public function authorize(): bool
    {
        return true;
    }

    /**
     * @return array{0: string, 1: array<string, mixed>}
     */
    public function handle(string $pluginName): array
    {
        return ["$pluginName::menu", []];
    }
}
