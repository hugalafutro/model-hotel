package com.hugalafutro.bellhop.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.PrefsStore
import com.hugalafutro.bellhop.data.QuotaBadgeConfig
import com.hugalafutro.bellhop.data.QuotaBadgeConfigStore
import com.hugalafutro.bellhop.data.QuotaBarMode
import com.hugalafutro.bellhop.data.QuotaSurface
import com.hugalafutro.bellhop.ui.common.FilterPill
import com.hugalafutro.bellhop.ui.common.ReorderableColumn
import com.hugalafutro.bellhop.ui.common.bellhopSwitchColors
import com.hugalafutro.bellhop.ui.common.moveItem
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import kotlinx.coroutines.launch

/**
 * QuotaBadgesConfigScreen lets the user reorder and hide quota badges
 * independently for the two surfaces that render them -- the dashboard strip
 * and the home-screen widget -- plus one global remaining/used mode that
 * governs the label both surfaces show for METERED providers
 * ([PrefsStore.quotaBarMode], NOT per-surface: see the task's ADDENDUM).
 *
 * Unlike the other settings screens, this one owns its store writes
 * directly rather than routing through a ViewModel + callbacks: [configStore]
 * and [prefsStore] are read live via `collectAsState` and every mutation
 * (`setOrder`, `setVisible`, `setQuotaBarMode`) is fired from a
 * [rememberCoroutineScope] launch, since there is no other state here that
 * needs a ViewModel's lifecycle-scoped coordination.
 *
 * Each [QuotaSurface] tab shows its own [QuotaBadgeConfig.order] as a
 * [ReorderableColumn] of rows, each with a visibility [Switch] bound to
 * [QuotaBadgeConfig.hidden]. Badge identity is always the provider **name**
 * (see global-constraints) -- never type or index. An empty order (no
 * quota-capable providers reconciled into this surface yet) renders a short
 * empty-state line instead of an empty list.
 */
@Composable
fun QuotaBadgesConfigScreen(
    configStore: QuotaBadgeConfigStore,
    prefsStore: PrefsStore,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val scope = rememberCoroutineScope()
    var surface by remember { mutableStateOf(QuotaSurface.MAIN) }

    // configStore.config(surface) builds a fresh Flow wrapper each call, so it's
    // remembered per-surface -- otherwise an unrelated recomposition would swap
    // the collected Flow's identity and force collectAsState to resubscribe.
    val configFlow = remember(configStore, surface) { configStore.config(surface) }
    val config by configFlow.collectAsState(initial = QuotaBadgeConfig())
    val mode by prefsStore.quotaBarMode.collectAsState(initial = QuotaBarMode.REMAINING)

    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 16.dp),
        ) {
            Spacer(modifier = Modifier.height(8.dp))
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            ) {
                IconButton(onClick = onBack, modifier = Modifier.testTag("quota-config-back")) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.quota_config_back),
                    )
                }
                Text(
                    text = stringResource(R.string.quota_config_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.weight(1f).testTag("quota-config-title"),
                )
            }

            ModeToggleRow(
                mode = mode,
                onSelect = { selected -> scope.launch { prefsStore.setQuotaBarMode(selected) } },
            )

            Spacer(modifier = Modifier.height(16.dp))

            SurfaceTabsRow(surface = surface, onSelect = { surface = it })

            // How much of what the fleet offers this surface actually shows. On
            // either tab, since "6 of 8" is the same question on the dashboard as
            // on the widget and only one of them used to answer it.
            val visibleCount = config.order.count { it !in config.hidden }
            if (config.order.isNotEmpty()) {
                Text(
                    text = stringResource(R.string.quota_config_selected, visibleCount, config.order.size),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 8.dp).testTag("quota-config-count"),
                )
            }

            Spacer(modifier = Modifier.height(8.dp))

            if (config.order.isEmpty()) {
                Text(
                    text = stringResource(R.string.quota_config_empty),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 8.dp).testTag("quota-config-empty"),
                )
            } else {
                ReorderableColumn(
                    items = config.order,
                    onMove = { from, to ->
                        scope.launch { configStore.setOrder(surface, moveItem(config.order, from, to)) }
                    },
                    modifier = Modifier.fillMaxWidth().weight(1f),
                ) { name, dragHandle ->
                    BadgeRow(
                        name = name,
                        visible = name !in config.hidden,
                        onVisibleChange = { checked ->
                            scope.launch { configStore.setVisible(surface, name, checked) }
                        },
                        dragHandle = dragHandle,
                    )
                }
            }
        }
    }
}

@Composable
private fun ModeToggleRow(
    mode: QuotaBarMode,
    onSelect: (QuotaBarMode) -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(modifier = modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                text = stringResource(R.string.quota_config_mode_title),
                style = MaterialTheme.typography.titleMedium,
            )
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                modifier = Modifier.fillMaxWidth().testTag("quota-mode-toggle"),
            ) {
                FilterPill(
                    text = stringResource(R.string.quota_config_mode_remaining),
                    selected = mode == QuotaBarMode.REMAINING,
                    onClick = { onSelect(QuotaBarMode.REMAINING) },
                    tag = "quota-mode-remaining",
                    modifier = Modifier.weight(1f),
                    borderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                FilterPill(
                    text = stringResource(R.string.quota_config_mode_used),
                    selected = mode == QuotaBarMode.USED,
                    onClick = { onSelect(QuotaBarMode.USED) },
                    tag = "quota-mode-used",
                    modifier = Modifier.weight(1f),
                    borderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun SurfaceTabsRow(
    surface: QuotaSurface,
    onSelect: (QuotaSurface) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        modifier = modifier.fillMaxWidth(),
    ) {
        FilterPill(
            text = stringResource(R.string.quota_config_tab_main),
            selected = surface == QuotaSurface.MAIN,
            onClick = { onSelect(QuotaSurface.MAIN) },
            tag = "quota-config-tab-main",
            modifier = Modifier.weight(1f),
        )
        FilterPill(
            text = stringResource(R.string.quota_config_tab_widget),
            selected = surface == QuotaSurface.WIDGET,
            onClick = { onSelect(QuotaSurface.WIDGET) },
            tag = "quota-config-tab-widget",
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun BadgeRow(
    name: String,
    visible: Boolean,
    onVisibleChange: (Boolean) -> Unit,
    dragHandle: Modifier,
    modifier: Modifier = Modifier,
) {
    // A divider-closed row rather than a card: this list is the same kind of
    // list Settings is, and eight stacked cards say nothing eight rules don't.
    Column(modifier = modifier.fillMaxWidth().testTag("quota-config-row-$name")) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(
                painter = painterResource(R.drawable.ic_drag_handle),
                contentDescription = stringResource(R.string.quota_config_reorder, name),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = dragHandle.testTag("quota-config-drag-$name"),
            )
            Text(
                text = name,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Switch(
                checked = visible,
                onCheckedChange = onVisibleChange,
                colors = bellhopSwitchColors(),
                modifier = Modifier.testTag("quota-config-visible-$name"),
            )
        }
        HorizontalDivider()
    }
}

@Preview(showBackground = true)
@Composable
private fun QuotaBadgesConfigScreenPreview() {
    BellhopTheme {
        // Preview only: real usage always passes live stores.
        Column(modifier = Modifier.padding(16.dp)) {
            Text("Quota badges preview needs live stores")
        }
    }
}
