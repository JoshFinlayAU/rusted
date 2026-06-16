<?php

namespace AthenaNetworks\RustedLibrenms\Hooks;

use App\Models\Device;
use App\Models\User;
use Illuminate\Contracts\View\View;
use LibreNMS\Interfaces\Plugins\Hooks\DeviceOverviewHook;

/**
 * Renders a "Configuration Backups" panel on every device's Overview page.
 *
 * The panel itself is loaded by LibreNMS server-side; its contents are then
 * populated by the browser via the same-origin AJAX receiver, keyed on the
 * device hostname (which is matched against the rusted device name).
 */
class DeviceOverview implements DeviceOverviewHook
{
    public function authorize(User $user, Device $device): bool
    {
        return true;
    }

    public function handle(string $pluginName, array $settings, Device $device): View
    {
        return view("$pluginName::device-overview", [
            'hostname' => $device->hostname,
        ]);
    }
}
