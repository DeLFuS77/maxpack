# SPDX-License-Identifier: GPL-3.0-or-later
cmake_minimum_required(VERSION 3.20)
project(maxpack VERSION 0.0.1 LANGUAGES C)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_C_EXTENSIONS OFF)

include(cmake/ThirdParty.cmake)
find_package(Threads REQUIRED)

function(mp_warnings tgt)
  if(MSVC)
    target_compile_options(${tgt} PRIVATE /W4 /utf-8)
    target_compile_definitions(${tgt} PRIVATE _CRT_SECURE_NO_WARNINGS)
  else()
    target_compile_options(${tgt} PRIVATE -Wall -Wextra)
  endif()
endfunction()

file(GLOB MP_CORE_SRC CONFIGURE_DEPENDS ${CMAKE_CURRENT_SOURCE_DIR}/src/core/*.c)
add_library(maxpack_core STATIC ${MP_CORE_SRC})
target_include_directories(maxpack_core PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/src/core)
target_link_libraries(maxpack_core PUBLIC mp_zstd mp_zlib Threads::Threads)
mp_warnings(maxpack_core)

file(GLOB MP_ANALYZE_SRC CONFIGURE_DEPENDS ${CMAKE_CURRENT_SOURCE_DIR}/src/analyze/*.c)
add_library(maxpack_analyze STATIC ${MP_ANALYZE_SRC})
target_include_directories(maxpack_analyze PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/src/analyze)
target_link_libraries(maxpack_analyze PUBLIC maxpack_core)
if(NOT MSVC)
  target_link_libraries(maxpack_analyze PUBLIC m)
endif()
mp_warnings(maxpack_analyze)

add_executable(maxpack src/cli/main.c)
target_link_libraries(maxpack PRIVATE maxpack_analyze)
mp_warnings(maxpack)

option(MAXPACK_BUILD_TESTS "Compilar las pruebas" ON)
if(MAXPACK_BUILD_TESTS)
  enable_testing()
  find_package(Python3 3.8 COMPONENTS Interpreter REQUIRED)
  set(MP_FIXTURES_DIR ${CMAKE_BINARY_DIR}/fixtures)
  set(MP_TEST_TMP ${CMAKE_BINARY_DIR}/tmp)

  # mp_add_test(<nombre> [FIXTURES]): compila tests/<nombre>.c y lo registra en CTest.
  function(mp_add_test name)
    cmake_parse_arguments(T "FIXTURES" "" "" ${ARGN})
    add_executable(${name} tests/${name}.c)
    target_link_libraries(${name} PRIVATE maxpack_analyze)
    target_include_directories(${name} PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/tests)
    target_compile_definitions(${name} PRIVATE
      MP_TEST_TMP="${MP_TEST_TMP}" MP_FIXTURES_DIR="${MP_FIXTURES_DIR}")
    mp_warnings(${name})
    add_test(NAME ${name} COMMAND ${name})
    set_tests_properties(${name} PROPERTIES TIMEOUT 300)
    if(T_FIXTURES)
      set_tests_properties(${name} PROPERTIES FIXTURES_REQUIRED exfat)
    endif()
  endfunction()

  # Fixtures exFAT sintéticas (Python) y contraste del lector de referencia.
  add_test(NAME gen_fixtures COMMAND ${Python3_EXECUTABLE} -I
    ${CMAKE_CURRENT_SOURCE_DIR}/tests/tools/make_exfat.py --out ${MP_FIXTURES_DIR})
  set_tests_properties(gen_fixtures PROPERTIES FIXTURES_SETUP exfat)
  add_test(NAME check_ref COMMAND ${Python3_EXECUTABLE} -I
    ${CMAKE_CURRENT_SOURCE_DIR}/tests/tools/check_ref.py ${MP_FIXTURES_DIR})
  set_tests_properties(check_ref PROPERTIES FIXTURES_REQUIRED exfat)

  mp_add_test(test_smoke)
  mp_add_test(test_platform)
  mp_add_test(test_source_dir)
  mp_add_test(test_extent)
  mp_add_test(test_source_exfat FIXTURES)
  set_tests_properties(test_source_exfat PROPERTIES TIMEOUT 10)
  mp_add_test(test_rules)
  mp_add_test(test_sample)

  # Herramienta de prueba: vuelca un rango de un archivo de un origen a stdout.
  add_executable(mp_cat tests/tools/mp_cat.c)
  target_link_libraries(mp_cat PRIVATE maxpack_core)
  mp_warnings(mp_cat)
endif()
