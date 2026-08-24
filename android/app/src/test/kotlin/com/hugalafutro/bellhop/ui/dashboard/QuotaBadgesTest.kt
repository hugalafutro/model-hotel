package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.data.NeuralWattBalance
import com.hugalafutro.bellhop.data.NeuralWattSubscription
import com.hugalafutro.bellhop.data.ProviderQuota
import com.hugalafutro.bellhop.data.QuotaBarMode
import com.hugalafutro.bellhop.data.QuotaData
import com.hugalafutro.bellhop.data.QuotaType
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * QuotaBadgeRow / QuotaDetailSheet: badge identity is the provider *name*
 * (never type or index, per global-constraints), so every assertion here is
 * keyed off [ProviderQuota.providerName]. Asserts on testTag, not translated
 * copy -- English strings never break these. Covers the brief's data/shape
 * parity contract: an available provider's badge shows its computed label,
 * a dead-key (available == false) one shows "-", and a tap -- on either --
 * fires the click callback with the provider's name.
 */
@RunWith(RobolectricTestRunner::class)
class QuotaBadgesTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private val available =
        ProviderQuota(
            providerName = "openrouter-main",
            type = QuotaType.OPENROUTER,
            data = QuotaData.OpenRouter(creditsRemaining = 12.5),
            fetchedAt = "2026-07-24T00:00:00Z",
            available = true,
        )

    private val deadKey =
        ProviderQuota(
            providerName = "nanogpt-broken",
            type = QuotaType.NANOGPT,
            data = null,
            fetchedAt = "",
            available = false,
        )

    @Test
    fun showsOneBadgePerProvider() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgeRow(quota = listOf(available, deadKey), mode = QuotaBarMode.REMAINING, onBadgeClick = {})
            }
        }
        composeTestRule.onNodeWithTag("quota-badge-openrouter-main").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-badge-nanogpt-broken").assertIsDisplayed()
    }

    @Test
    fun deadKeyBadgeRendersDash() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgeRow(quota = listOf(deadKey), mode = QuotaBarMode.REMAINING, onBadgeClick = {})
            }
        }
        composeTestRule.onNodeWithTag("quota-badge-nanogpt-broken").assertTextContains("-", substring = true)
    }

    @Test
    fun availableBadgeShowsComputedLabel() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgeRow(quota = listOf(available), mode = QuotaBarMode.REMAINING, onBadgeClick = {})
            }
        }
        // quotaBadgeLabel formats OpenRouter as "$<creditsRemaining, 2dp>".
        composeTestRule.onNodeWithTag("quota-badge-openrouter-main").assertTextContains("$12.50", substring = true)
    }

    @Test
    fun tappingAvailableBadgeFiresCallbackWithProviderName() {
        var clicked: String? = null
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgeRow(
                    quota = listOf(available, deadKey),
                    mode = QuotaBarMode.REMAINING,
                    onBadgeClick = { clicked = it },
                )
            }
        }
        composeTestRule.onNodeWithTag("quota-badge-openrouter-main").performClick()
        assertEquals("openrouter-main", clicked)
    }

    // A dead-key badge stays tappable: the detail sheet is what explains
    // *why* it's unavailable, so swallowing the tap would strand the user
    // looking at a "-" with no way to find out more.
    @Test
    fun tappingDeadKeyBadgeAlsoFiresCallback() {
        var clicked: String? = null
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgeRow(
                    quota = listOf(available, deadKey),
                    mode = QuotaBarMode.REMAINING,
                    onBadgeClick = { clicked = it },
                )
            }
        }
        composeTestRule.onNodeWithTag("quota-badge-nanogpt-broken").performClick()
        assertEquals("nanogpt-broken", clicked)
    }

    @Test
    fun emptyQuotaRendersNoBadges() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgeRow(quota = emptyList(), mode = QuotaBarMode.REMAINING, onBadgeClick = {})
            }
        }
        composeTestRule.onNodeWithTag("quota-badge-openrouter-main").assertDoesNotExist()
    }

    @Test
    fun detailSheetShowsUnavailableLineForDeadKey() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaDetailSheet(pq = deadKey, mode = QuotaBarMode.REMAINING, onDismiss = {})
            }
        }
        composeTestRule.onNodeWithTag("quota-detail-sheet").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-detail-unavailable").assertIsDisplayed()
    }

    @Test
    fun detailSheetShowsComputedLabelForAvailableProvider() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaDetailSheet(pq = available, mode = QuotaBarMode.REMAINING, onDismiss = {})
            }
        }
        composeTestRule.onNodeWithText("$12.50", substring = true).assertIsDisplayed()
    }

    @Test
    fun detailSheetDrawsABarPerMeteredReading() {
        val neuralWatt =
            ProviderQuota(
                providerName = "neuralwatt-main",
                type = QuotaType.NEURALWATT,
                data =
                    QuotaData.NeuralWatt(
                        balance = NeuralWattBalance(creditsUsedUsd = 3.0, totalCreditsUsd = 12.0),
                        subscription = NeuralWattSubscription(kwhIncluded = 20.0, kwhUsed = 12.5),
                    ),
                fetchedAt = "2026-07-26T00:00:00Z",
                available = true,
            )
        composeTestRule.setContent {
            BellhopTheme {
                QuotaDetailSheet(pq = neuralWatt, mode = QuotaBarMode.REMAINING, onDismiss = {})
            }
        }

        composeTestRule.onNodeWithTag("quota-detail-meter-ENERGY").assertIsDisplayed()
        // No credits bar on purpose: NeuralWatt's credits_used_usd is a
        // hardwired 0 and total re-bases to remaining as spend settles, so
        // the bar could only ever render as untouched.
        composeTestRule.onNodeWithTag("quota-detail-meter-CREDITS").assertDoesNotExist()
    }

    @Test
    fun detailSheetDrawsNoBarsWithoutACeiling() {
        // `available` is a pay-as-you-go OpenRouter key: spend, no credit
        // ceiling. A bar here would be a full-looking track with no meaning.
        composeTestRule.setContent {
            BellhopTheme {
                QuotaDetailSheet(pq = available, mode = QuotaBarMode.REMAINING, onDismiss = {})
            }
        }

        composeTestRule.onNodeWithTag("quota-detail-meter-CREDITS").assertDoesNotExist()
    }
}
