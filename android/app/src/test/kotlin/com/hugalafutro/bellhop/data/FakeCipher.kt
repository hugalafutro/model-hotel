package com.hugalafutro.bellhop.data

/**
 * FakeCipher stands in for [KeystoreCrypto] in tests (Robolectric has no
 * AndroidKeyStore provider). The "enc:" prefix proves the store actually runs
 * ciphertext through the cipher rather than persisting the token in the clear.
 */
object FakeCipher : TokenCipher {
    override fun encrypt(plaintext: String): String = "enc:$plaintext"

    override fun decrypt(stored: String): String? = if (stored.startsWith("enc:")) stored.removePrefix("enc:") else null
}
