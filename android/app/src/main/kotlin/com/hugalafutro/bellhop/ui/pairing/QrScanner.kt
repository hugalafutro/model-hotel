package com.hugalafutro.bellhop.ui.pairing

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.hugalafutro.bellhop.R
import com.journeyapps.barcodescanner.CameraPreview
import com.journeyapps.barcodescanner.CompoundBarcodeView

/**
 * QrScanner hosts the ZXing preview directly rather than launching its opaque
 * CaptureActivity. Owning the surface is what makes the outcomes deterministic:
 * a decoded QR arrives via [onScanned], and a camera that cannot be opened (no
 * hardware, service unavailable, held by another app) invokes ZXing's explicit
 * [CameraPreview.StateListener.cameraError] callback, which is reported through
 * [onCameraError] instead of being indistinguishable from a user cancel at the
 * result level. A denied CAMERA permission is likewise routed to [onCameraError]
 * so the caller can offer the paste fallback. The caller owns the cancel path
 * (e.g. a BackHandler): unmounting this composable releases the camera.
 */
@Composable
fun QrScanner(
    onScanned: (String) -> Unit,
    onCameraError: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    var granted by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) ==
                PackageManager.PERMISSION_GRANTED,
        )
    }
    val permissionLauncher =
        rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { isGranted ->
            if (isGranted) granted = true else onCameraError()
        }
    LaunchedEffect(Unit) {
        if (!granted) permissionLauncher.launch(Manifest.permission.CAMERA)
    }
    if (!granted) return

    val prompt = stringResource(R.string.pairing_scan_prompt)
    val barcodeView =
        remember {
            CompoundBarcodeView(context).apply {
                setStatusText(prompt)
                barcodeView.addStateListener(
                    object : CameraPreview.StateListener {
                        override fun previewSized() = Unit

                        override fun previewStarted() = Unit

                        override fun previewStopped() = Unit

                        override fun cameraClosed() = Unit

                        // The one signal ZXing's CaptureActivity swallows: a
                        // camera it could not open. Surfacing it here is the whole
                        // point of hosting the preview ourselves.
                        override fun cameraError(error: Exception) = onCameraError()
                    },
                )
                decodeSingle { result -> result.text?.let(onScanned) }
            }
        }

    // Mirror the host lifecycle so the camera is released in the background and
    // re-acquired on return, and always released when this leaves composition.
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer =
            LifecycleEventObserver { _, event ->
                when (event) {
                    Lifecycle.Event.ON_RESUME -> barcodeView.resume()
                    Lifecycle.Event.ON_PAUSE -> barcodeView.pause()
                    else -> Unit
                }
            }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose {
            lifecycleOwner.lifecycle.removeObserver(observer)
            barcodeView.pause()
        }
    }

    AndroidView(factory = { barcodeView }, modifier = modifier.fillMaxSize())
}
