<?php

namespace AthenaNetworks\RustedLibrenms;

use Illuminate\Support\ServiceProvider;
use LibreNMS\Interfaces\Plugins\Hooks\DeviceOverviewHook;
use LibreNMS\Interfaces\Plugins\Hooks\MenuEntryHook;
use LibreNMS\Interfaces\Plugins\Hooks\SettingsHook;
use LibreNMS\Interfaces\Plugins\PluginManagerInterface;
use AthenaNetworks\RustedLibrenms\Hooks\DeviceOverview;
use AthenaNetworks\RustedLibrenms\Hooks\MenuEntry;
use AthenaNetworks\RustedLibrenms\Hooks\Settings;

class RustedServiceProvider extends ServiceProvider
{
    /** The plugin name as known to LibreNMS (also the view/route namespace). */
    public const PLUGIN = 'rusted';

    public function register(): void
    {
        // Make config('rusted-librenms.*') available without publishing.
        $this->mergeConfigFrom(__DIR__.'/../config/config.php', 'rusted-librenms');
    }

    public function boot(PluginManagerInterface $pluginManager): void
    {
        // Register hooks so LibreNMS surfaces the plugin in its UI.
        $pluginManager->publishHook(self::PLUGIN, MenuEntryHook::class, MenuEntry::class);
        $pluginManager->publishHook(self::PLUGIN, SettingsHook::class, Settings::class);
        $pluginManager->publishHook(self::PLUGIN, DeviceOverviewHook::class, DeviceOverview::class);

        if (! $pluginManager->pluginEnabled(self::PLUGIN)) {
            return;
        }

        $this->loadRoutesFrom(__DIR__.'/../routes/web.php');
        $this->loadViewsFrom(__DIR__.'/../resources/views', self::PLUGIN);

        $this->publishes([
            __DIR__.'/../config/config.php' => config_path('rusted-librenms.php'),
        ], 'rusted-librenms-config');
    }
}
