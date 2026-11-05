package com.androidmac.client.protocol

import org.json.JSONArray
import org.json.JSONObject

data class ScreenInfo(val width: Int, val height: Int, val dpi: Int)

data class ClientHello(
    val device: String,
    val screen: ScreenInfo,
    val capabilities: List<String>,
    val codecs: List<String>,
    val connectionType: String = "wifi"
) {
    fun toJson(): String {
        val obj = JSONObject()
        obj.put("device", device)
        obj.put("screen", JSONObject().apply {
            put("width", screen.width)
            put("height", screen.height)
            put("dpi", screen.dpi)
        })
        obj.put("capabilities", JSONArray(capabilities))
        obj.put("codecs", JSONArray(codecs))
        if (connectionType != "wifi") {
            obj.put("connectionType", connectionType)
        }
        return obj.toString()
    }
}

data class DisplayInfo(val width: Int, val height: Int)

data class ServerHello(
    val virtualDisplay: DisplayInfo,
    val codec: String,
    val bitrate: Int,
    val fps: Int,
    val streamPort: Int,
    val touchPort: Int,
    val videoPort: Int = 0
) {
    companion object {
        fun fromJson(json: String): ServerHello {
            val obj = JSONObject(json)
            val disp = obj.getJSONObject("virtualDisplay")
            return ServerHello(
                virtualDisplay = DisplayInfo(disp.getInt("width"), disp.getInt("height")),
                codec = obj.getString("codec"),
                bitrate = obj.getInt("bitrate"),
                fps = obj.getInt("fps"),
                streamPort = obj.getInt("streamPort"),
                touchPort = obj.optInt("touchPort", 0),
                videoPort = obj.optInt("videoPort", 0)
            )
        }
    }
}

data class ClientReady(val udpPort: Int) {
    fun toJson(): String {
        val obj = JSONObject()
        obj.put("udpPort", udpPort)
        return obj.toString()
    }
}
