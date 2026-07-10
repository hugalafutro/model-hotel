package com.hugalafutro.bellhop

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.hugalafutro.bellhop.ui.dashboard.DashboardScreen
import com.hugalafutro.bellhop.ui.theme.BellhopTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            BellhopTheme {
                DashboardScreen()
            }
        }
    }
}
