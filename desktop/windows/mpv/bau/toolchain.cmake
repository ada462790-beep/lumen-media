# Lumen — eigener Streaming-Server fürs Heimnetz.
# Copyright (C) 2026 Die Lumen-Autoren
# SPDX-License-Identifier: AGPL-3.0-only
#
# CMake-Toolchain für den libmpv-Bau (bauen.sh): shaderc und SPIRV-Cross
# für Windows x86-64 mit dem MinGW-w64-GCC aus Debian.
set(CMAKE_SYSTEM_NAME Windows)
set(CMAKE_SYSTEM_PROCESSOR x86_64)
set(CMAKE_C_COMPILER x86_64-w64-mingw32-gcc-posix)
set(CMAKE_CXX_COMPILER x86_64-w64-mingw32-g++-posix)
set(CMAKE_RC_COMPILER x86_64-w64-mingw32-windres)
set(CMAKE_AR x86_64-w64-mingw32-gcc-ar-posix CACHE FILEPATH "")
set(CMAKE_RANLIB x86_64-w64-mingw32-gcc-ranlib-posix CACHE FILEPATH "")
set(CMAKE_FIND_ROOT_PATH /usr/x86_64-w64-mingw32 /bau/prefix)
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_PACKAGE ONLY)
